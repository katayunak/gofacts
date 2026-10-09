package scan

// These are testigo's scanningFlow tests, ported. They are the contract the
// shared package has to keep for testigo to switch to it.

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/katayunak/1819/gofacts/fact"
)

func TestFindsPlantedDefects(t *testing.T) {
	flow := paysvcFlow(t)
	for _, w := range []struct {
		id, file string
		sev      fact.Severity
	}{
		{"MONEY-FLOAT", "domain/payment.go", fact.SevCritical},
		{"MONEY-FLOAT", "api/server.go", fact.SevCritical},
		{"MONEY-DIV", "api/server.go", fact.SevHigh},
		{"MONEY-NO-CURRENCY", "domain/payment.go", fact.SevMedium},
		{"TX-NET-CALL", "api/server.go", fact.SevCritical},
		{"TX-NO-ROLLBACK", "api/server.go", fact.SevHigh},
		{"STATE-NEVER-SET", "domain/payment.go", fact.SevMedium},
		{"IDEM-KEY-NOT-UNIQUE", "domain/payment.go", fact.SevCritical},
	} {
		found := false
		for _, f := range flow.Findings {
			if f.ID == w.id && f.Ref.File == w.file {
				found = true
				if f.Severity != w.sev {
					t.Errorf("%s in %s: severity %s, want %s", w.id, w.file, f.Severity, w.sev)
				}
				break
			}
		}
		if !found {
			t.Errorf("missed planted defect %s in %s", w.id, w.file)
		}
	}
}

func TestDoesNotReportNoise(t *testing.T) {
	banned := map[string]string{
		"(error).Error":                   "CHA unions every error implementation; formatting an error is not a seam",
		"(io.Closer).Close":               "language plumbing",
		"(net/http.ResponseWriter).Write": "server-side response writing is not an outbound call",
		"(net/http.Header).Get":           "reading a header is not an HTTP request",
		"(*net/url.URL).Query":            "parsing a query string is not I/O",
		"(*database/sql.Rows).Next":       "cursor mechanics; the query is the seam",
		"(*database/sql.Rows).Scan":       "cursor mechanics",
	}
	for _, s := range paysvcFlow(t).Seams {
		if why, bad := banned[s.Target]; bad {
			t.Errorf("reported %s as a %s seam in %s — %s", s.Target, s.Kind, s.In.Symbol, why)
		}
	}
}

func TestInjectabilityIsCorrect(t *testing.T) {
	byTarget := map[string]fact.Seam{}
	for _, s := range paysvcFlow(t).Seams {
		byTarget[s.Target] = s
	}
	for target, want := range map[string]bool{
		"(example.com/paysvc/psp.Gateway).Authorize": true,
		"(example.com/paysvc/ledger.Ledger).Post":    true,
		"(*database/sql.DB).BeginTx":                 false,
		"(*net/http.Client).Do":                      false,
		"time.Now":                                   false,
	} {
		s, ok := byTarget[target]
		if !ok {
			t.Errorf("seam %s not found", target)
			continue
		}
		if s.Injectable != want {
			t.Errorf("%s: injectable=%v, want %v", target, s.Injectable, want)
		}
	}
}

func TestStateMachineExtraction(t *testing.T) {
	flow := paysvcFlow(t)
	if machine(flow.States, ".DeclineCode") != nil {
		t.Error("DeclineCode is an enum, not a lifecycle: never stored, never advanced")
	}
	if machine(flow.States, ".SettlementMode") == nil {
		t.Error("SettlementMode is a weak name but a real lifecycle")
	}
	m := machine(flow.States, ".PaymentStatus")
	if m == nil {
		t.Fatal("PaymentStatus machine missing")
	}
	if m.Field != "Status" {
		t.Errorf("field = %q", m.Field)
	}
	want := []string{"StatusAbandoned", "StatusAuthorized", "StatusCaptured", "StatusFailed", "StatusPending", "StatusRefunded"}
	if !slices.Equal(m.States, want) {
		t.Errorf("states = %v", m.States)
	}
	written := map[string]string{}
	for _, w := range m.Writes {
		written[w.To] = w.In.Symbol
	}
	for state, in := range map[string]string{
		"StatusPending":    "(*Server).process",
		"StatusAuthorized": "(*Server).process",
		"StatusCaptured":   "(*Handler).capture",
		"StatusFailed":     "(*Job).Reconcile",
	} {
		if written[state] != in {
			t.Errorf("%s written in %q, want %q", state, written[state], in)
		}
	}
	for _, never := range []string{"StatusRefunded", "StatusAbandoned"} {
		if _, ok := written[never]; ok {
			t.Errorf("%s has no write site in the fixture", never)
		}
	}
}

func TestAllEntryPointsAreFollowed(t *testing.T) {
	flow := paysvcFlow(t)
	for id, pos := range map[string]fact.NodePosition{
		"example.com/paysvc/api#(*Server).CreatePayment":    fact.NodePositionEntry,
		"example.com/paysvc/webhook#(*Handler).PSPCallback": fact.NodePositionEntry,
		"example.com/paysvc/recon#(*Job).Reconcile":         fact.NodePositionEntry,
		"example.com/paysvc/api#(*Server).process":          fact.NodePositionInternal,
		"example.com/paysvc/webhook#(*Handler).capture":     fact.NodePositionInternal,
		"example.com/paysvc/ledger#(*SQLLedger).Post":       fact.NodePositionLeaf,
		"example.com/paysvc/psp#(*HTTPGateway).Authorize":   fact.NodePositionLeaf,
	} {
		n, ok := flow.Nodes[id]
		if !ok {
			t.Errorf("node %s missing", id)
			continue
		}
		if n.Position != pos {
			t.Errorf("%s: %s, want %s", id, n.Position, pos)
		}
	}
	if n := flow.Nodes["example.com/paysvc/webhook#(*Handler).PSPCallback"]; n == nil || !n.Facts.SpawnsGoroutine {
		t.Error("PSPCallback spawns a goroutine")
	}
	if n := flow.Nodes["example.com/paysvc/api#(*Server).process"]; n == nil || !n.Facts.OpensTx || !n.Facts.CommitsTx || n.Facts.RollsBackTx {
		t.Errorf("process tx facts wrong: %+v", n)
	}
}

func TestUnreachedFunctionsAreExcluded(t *testing.T) {
	flow := paysvcFlow(t)
	for _, id := range []string{"example.com/paysvc/api#(*Server).splitFee", "example.com/paysvc/domain#(*Payment).Terminal"} {
		if _, ok := flow.Nodes[id]; ok {
			t.Errorf("%s is not reachable from any entry point", id)
		}
	}
	if len(findings(flow.Findings, "MONEY-DIV")) == 0 {
		t.Error("MONEY-DIV is about a type, not a path: it is reported even though splitFee is unreachable")
	}
}

func TestNodeCodeRefsHaveFilePaths(t *testing.T) {
	for id, n := range paysvcFlow(t).Nodes {
		if n.Ref.File == "" || filepath.IsAbs(n.Ref.File) {
			t.Errorf("%s: file %q must be repo-relative", id, n.Ref.File)
		}
	}
}

func TestNoCalleeIsListedTwice(t *testing.T) {
	for id, n := range paysvcFlow(t).Nodes {
		seen := map[string]bool{}
		for _, c := range n.Calls {
			if seen[c] {
				t.Errorf("%s lists %s twice", id, c)
			}
			seen[c] = true
		}
	}
}

func TestDeclarationOrderIsIgnored(t *testing.T) {
	p := repo(t, map[string]string{"svc/svc.go": `package svc

import "context"

func Last(ctx context.Context) error  { return nil }

func Third(ctx context.Context) error { return Last(ctx) }

func Second(ctx context.Context) error { return Third(ctx) }

func Entry(ctx context.Context) error { return Second(ctx) }
`})
	flow, err := p.Flow([]fact.EntryPoint{{Pkg: "example.com/m/svc", Symbol: "Entry"}})
	if err != nil {
		t.Fatal(err)
	}
	chain := []string{"Entry", "Second", "Third", "Last"}
	for i := 0; i < len(chain)-1; i++ {
		n := flow.Nodes["example.com/m/svc#"+chain[i]]
		if n == nil || !slices.Equal(n.Calls, []string{"example.com/m/svc#" + chain[i+1]}) {
			t.Errorf("%s: %+v", chain[i], n)
		}
	}
}

func TestCallsWithinABodyFollowSourceOrder(t *testing.T) {
	p := repo(t, map[string]string{"svc/svc.go": `package svc

func zulu() {}
func yankee() {}
func xray() {}

func Flow() {
	xray()
	zulu()
	yankee()
}
`})
	flow, err := p.Flow([]fact.EntryPoint{{Pkg: "example.com/m/svc", Symbol: "Flow"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/m/svc#xray", "example.com/m/svc#zulu", "example.com/m/svc#yankee"}
	if got := flow.Nodes["example.com/m/svc#Flow"].Calls; !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func TestFindingsAgreeWithTheScorer(t *testing.T) {
	p := repo(t, map[string]string{
		"migrations/0001.sql": "CREATE TABLE payments (\n  id TEXT PRIMARY KEY,\n  order_id TEXT NOT NULL\n);\n",
		"domain/domain.go": `package domain

type Caps struct {
	AcceptsDedupKey  bool
	DedupKeyArgument []string
}

type Payment struct {
	ID      string
	OrderID string
}
`,
		"api/api.go": `package api

import (
	"context"
	"net/http"

	"example.com/m/domain"
)

type Server struct{ caps domain.Caps }

func (s *Server) Handle(ctx context.Context, r *http.Request) error {
	p := &domain.Payment{OrderID: r.Header.Get("X-Order-Id")}
	_ = p
	return nil
}
`,
	})
	for _, f := range findings(p.Findings(), "IDEM-KEY-NOT-UNIQUE") {
		if strings.Contains(f.Title, "AcceptsDedupKey") || strings.Contains(f.Title, "DedupKeyArgument") {
			t.Errorf("reported a non-key: %s", f.Title)
		}
	}
	for _, c := range p.IdempotencyKeys() {
		if c.Name == "AcceptsDedupKey" || c.Name == "DedupKeyArgument" {
			t.Errorf("%s cannot hold a key but was scored", c.Name)
		}
	}
}

func TestReturnedStateIsNotDead(t *testing.T) {
	p := repo(t, map[string]string{
		"domain/domain.go": `package domain

type Status string

const (
	StatusNew      Status = "new"
	StatusSettled  Status = "settled"
	StatusVoided   Status = "voided"
	StatusDisputed Status = "disputed"
	StatusGhost    Status = "ghost"
)

type Payment struct {
	ID     string
	Status Status
}
`,
		"api/api.go": `package api

import "example.com/m/domain"

type Server struct{}

func (s *Server) Settle() domain.Status { return domain.StatusSettled }

func (s *Server) Void(id string) *domain.Payment {
	return &domain.Payment{ID: id, Status: domain.StatusVoided}
}

func (s *Server) Dispute() domain.Status {
	st := domain.StatusDisputed
	return st
}
`,
	})
	m := machine(p.States(), "domain.Status")
	if m == nil {
		t.Fatal("domain.Status was not detected")
	}
	dead := map[string]bool{}
	for _, s := range m.NeverAssigned {
		dead[s] = true
	}
	for _, live := range []string{"StatusSettled", "StatusVoided", "StatusDisputed"} {
		if dead[live] {
			t.Errorf("%s is produced in the source", live)
		}
	}
	if !dead["StatusGhost"] || !dead["StatusNew"] {
		t.Error("StatusGhost and StatusNew are never produced")
	}
}

func TestNameAloneIsNotALifecycle(t *testing.T) {
	p := repo(t, map[string]string{
		"domain/domain.go": `package domain

type Phase string

const (
	PhaseOne Phase = "one"
	PhaseTwo Phase = "two"
)

type PaymentStatus string

const (
	StatusPending PaymentStatus = "pending"
	StatusDone    PaymentStatus = "done"
)

type Payment struct {
	Status PaymentStatus
}
`,
		"api/api.go": `package api

import (
	"fmt"

	"example.com/m/domain"
)

func Handle(p *domain.Payment) error {
	fmt.Println(domain.PhaseOne)
	p.Status = domain.StatusPending
	return nil
}
`,
	})
	if machine(p.States(), "domain.Phase") != nil {
		t.Error("Phase is only printed; passing it to fmt.Println is not a write")
	}
	if machine(p.States(), "domain.PaymentStatus") == nil {
		t.Error("PaymentStatus is assigned and must be detected")
	}
}

func TestSymbolNaming(t *testing.T) {
	p := repo(t, map[string]string{"s/s.go": `package s

type Service struct{}
type Repo[T any] struct{}

func Pay() {}
func (s Service) Pay() {}
func (s *Service) Refund() {}
func (r *Repo[T]) Save() {}
`})
	for _, sym := range []string{"Pay", "Service.Pay", "(*Service).Refund", "(*Repo).Save"} {
		if _, ok := p.Function("example.com/m/s#" + sym); !ok {
			t.Errorf("%s missing; have %v", sym, ids(p.Functions()))
		}
	}
}

func ids(fs []fact.Function) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Ref.ID())
	}
	return out
}
