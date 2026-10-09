package scan

// Each test here covers something testigo's scanner got wrong or could not
// see. The test names say what is now true; the failure messages say what
// testigo did.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/katayunak/1819/gofacts/fact"
)

const diService = `package service

import "context"

type Store interface {
	Save(ctx context.Context, id string) error
}

type Service struct{ Store Store }

func (s *Service) Pay(ctx context.Context, id string) error { return s.Store.Save(ctx, id) }
`

const diRepo = `package repo

import (
	"context"
	"database/sql"
)

type PG struct{ DB *sql.DB }

func (r *PG) Save(ctx context.Context, id string) error {
	_, err := r.DB.ExecContext(ctx, "insert into payments(id) values($1)", id)
	return err
}
`

const diMock = `package mocks

import "context"

type Store struct{}

func (Store) Save(ctx context.Context, id string) error { return nil }
`

func TestImplementationBehindAConsumerInterfaceIsReached(t *testing.T) {
	p := repo(t, map[string]string{
		"service/service.go": diService,
		"repo/repo.go":       diRepo,
		"mocks/mocks.go":     diMock,
		"cmd/app/main.go": `package main

import (
	"context"

	"example.com/m/repo"
	"example.com/m/service"
)

func main() {
	s := &service.Service{Store: &repo.PG{}}
	_ = s.Pay(context.Background(), "x")
}
`,
	})
	flow, err := p.Flow([]fact.EntryPoint{{Pkg: "example.com/m/service", Symbol: "(*Service).Pay"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := flow.Nodes["example.com/m/repo#(*PG).Save"]; !ok {
		t.Fatal("service never imports repo, so testigo's import-closure filter dropped this edge and the repository's SQL vanished from the flow")
	}
	if _, ok := flow.Nodes["example.com/m/mocks#Store.Save"]; ok {
		t.Error("main wires repo.PG; VTA sees that, so the unused mock is not a callee")
	}
	var edge fact.Edge
	for _, e := range p.Callees("example.com/m/service#(*Service).Pay") {
		if e.To == "example.com/m/repo#(*PG).Save" {
			edge = e
		}
	}
	if edge.Certainty != fact.Likely || edge.Kind != fact.CallDispatch {
		t.Errorf("edge = %+v, want a dispatch narrowed by VTA (likely)", edge)
	}
}

func TestWithoutWiringEveryImplementationIsAPossibleCallee(t *testing.T) {
	p := repo(t, map[string]string{
		"service/service.go": diService,
		"repo/repo.go":       diRepo,
		"mocks/mocks.go":     diMock,
	})
	got := map[string]fact.Certainty{}
	for _, e := range p.Callees("example.com/m/service#(*Service).Pay") {
		got[e.To] = e.Certainty
	}
	if got["example.com/m/repo#(*PG).Save"] != fact.Possible || got["example.com/m/mocks#Store.Save"] != fact.Possible {
		t.Fatalf("callees = %v: with no value seen flowing into Store, every implementation is possible and says so", got)
	}
}

func TestGoroutineHandOffIsMarkedOnTheEdge(t *testing.T) {
	p := fixture(t, "paysvc")
	for _, e := range p.Callees("example.com/paysvc/webhook#(*Handler).PSPCallback") {
		if e.To == "example.com/paysvc/webhook#(*Handler).capture" {
			if !e.Go {
				t.Error("capture runs in a goroutine the handler does not wait for; a call chain that hides that reads as synchronous")
			}
			return
		}
	}
	t.Fatal("PSPCallback -> capture edge missing")
}

// txLab holds one transaction scenario per file, so line numbers in the tests
// are the scenario file's own.
func txLab(t *testing.T) *Program {
	return lab(t, "tx", func() map[string]string {
		return withFakeGorm(map[string]string{
			"svc/s.go": `package svc

import (
	"database/sql"
	"net/http"
)

type S struct {
	DB     *sql.DB
	Client *http.Client
}
`,
			"svc/aftercommit.go": `package svc

import (
	"context"
	"net/http"
)

func (s *S) AfterCommit(ctx context.Context, req *http.Request) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "update payments set status = 'paid' where id = $1", 1); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = s.Client.Do(req)
	return err
}
`,
			"svc/viahelper.go": `package svc

import (
	"context"
	"net/http"
)

func (s *S) notify(req *http.Request) error {
	_, err := s.Client.Do(req)
	return err
}

func (s *S) ViaHelper(ctx context.Context, req *http.Request) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.notify(req); err != nil {
		return err
	}
	return tx.Commit()
}
`,
			"svc/leak.go": `package svc

import "context"

func (s *S) Leak(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "update payments set status = 'paid' where id = $1", 1); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
`,
			"svc/deferred.go": `package svc

import "context"

func (s *S) Deferred(ctx context.Context) (err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, "delete from holds where id = $1", 1); err != nil {
		return err
	}
	return tx.Commit()
}
`,
			"svc/bypass.go": `package svc

import "context"

func (s *S) Bypass(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "update payments set status = 'paid' where id = $1", 1); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, "insert into ledger(id) values($1)", 1); err != nil {
		return err
	}
	return tx.Commit()
}
`,
			"svc/lent.go": `package svc

import (
	"context"
	"database/sql"
)

func insert(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "insert into payments(id) values($1)", 1)
	return err
}

func (s *S) Lent(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := insert(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
`,
			"orm/orm.go": `package orm

import "gorm.io/gorm"

type Order struct{ ID string }

func Save(db *gorm.DB, o *Order) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return db.Create(o).Error
	})
}

func SaveRight(db *gorm.DB, o *Order) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Create(o).Error
	})
}
`,
		})
	})
}

func TestNetworkCallAfterCommitIsNotInsideTheTransaction(t *testing.T) {
	p := txLab(t)
	if f, ok := findingIn(p.Findings(), "TX-NET-CALL", "(*S).AfterCommit"); ok {
		t.Fatalf("the HTTP call runs after the commit; testigo's per-function flags reported it anyway: %+v", f)
	}
	if f, ok := findingIn(p.Findings(), "TX-NO-ROLLBACK", "(*S).AfterCommit"); ok {
		t.Fatalf("rollback is deferred: %+v", f)
	}
}

func TestNetworkCallThroughAHelperIsInsideTheTransaction(t *testing.T) {
	f, ok := findingIn(txLab(t).Findings(), "TX-NET-CALL", "(*S).ViaHelper")
	if !ok {
		t.Fatal("the network call is one helper away; testigo only looked at the function's own calls")
	}
	if f.Line != 19 || f.Ref.File != "svc/viahelper.go" || !strings.Contains(f.Detail, "(*S).notify") {
		t.Errorf("finding = %+v", f)
	}
}

func TestAReturnBetweenBeginAndCommitIsALeak(t *testing.T) {
	p := txLab(t)
	tx := txIn(p.Transactions(), "(*S).Leak")
	if tx == nil {
		t.Fatal("transaction not found")
	}
	if len(tx.Leaks) != 1 || tx.Leaks[0].Line != 11 {
		t.Fatalf("leaks = %+v; want only the return after the failed exec: the return after a failed begin has nothing to roll back, the one after commit nothing left", tx.Leaks)
	}
	if f, ok := findingIn(p.Findings(), "TX-NO-ROLLBACK", "(*S).Leak"); !ok || f.Line != 11 {
		t.Errorf("finding = %+v", f)
	}
}

func TestADeferredClosureRollbackCoversEveryReturn(t *testing.T) {
	tx := txIn(txLab(t).Transactions(), "(*S).Deferred")
	if tx == nil || !tx.DeferredRollback || len(tx.Leaks) != 0 || !tx.Used {
		t.Fatalf("tx = %+v", tx)
	}
}

func TestDatabaseWorkBesideTheTransactionIsReported(t *testing.T) {
	f, ok := findingIn(txLab(t).Findings(), "TX-BYPASS", "(*S).Bypass")
	if !ok {
		t.Fatal("the ledger insert goes to s.DB while the transaction is open; if the payment rolls back the ledger entry stays")
	}
	if f.Severity != fact.SevHigh || f.Basis != fact.Proved || f.Line != 14 {
		t.Errorf("finding = %+v", f)
	}
}

func TestATransactionLentToAHelperIsStillOwnedHere(t *testing.T) {
	p := txLab(t)
	tx := txIn(p.Transactions(), "(*S).Lent")
	if tx == nil || tx.Escapes || len(tx.Leaks) != 1 {
		t.Fatalf("a helper that neither commits nor rolls back only borrows the transaction: %+v", tx)
	}
	if _, ok := findingIn(p.Findings(), "TX-BYPASS", "(*S).Lent"); ok {
		t.Error("the helper is handed the transaction, so its insert is inside it")
	}
}

func TestGormCallbackWritingThroughTheOuterHandle(t *testing.T) {
	p := txLab(t)
	if _, ok := findingIn(p.Findings(), "TX-BYPASS", "Save"); !ok {
		t.Fatal("db.Create inside db.Transaction's callback writes outside the transaction it was meant to be in")
	}
	if f, ok := findingIn(p.Findings(), "TX-BYPASS", "SaveRight"); ok {
		t.Errorf("tx.Create is the right handle: %+v", f)
	}
	tx := txIn(p.Transactions(), "SaveRight")
	if tx == nil || !tx.Wrapper || !tx.Used {
		t.Errorf("wrapper tx = %+v", tx)
	}
}

func TestATransactionNobodyUsesProtectsNothing(t *testing.T) {
	p := fixture(t, "walletsvc")
	f, ok := findingIn(p.Findings(), "TX-UNUSED", "(*Store).Atomic")
	if !ok {
		t.Fatal("Atomic begins a transaction but runs fn(ctx) on the pool: every write inside it is outside the transaction, and testigo could not see that")
	}
	if f.Basis != fact.Proved || f.Ref.File != "repo/wallet.go" {
		t.Errorf("finding = %+v", f)
	}
	if _, ok := findingIn(p.Findings(), "TX-NO-ROLLBACK", "(*Store).Atomic"); !ok {
		t.Error("the return after fn fails leaves the transaction open")
	}
}

func TestLedgerPostOutsideThePaymentTransaction(t *testing.T) {
	f, ok := findingIn(fixture(t, "paysvc").Findings(), "TX-BYPASS", "(*Server).process")
	if !ok {
		t.Fatal("process posts to the ledger through an interface whose implementation uses its own *sql.DB, not the open transaction")
	}
	if f.Severity != fact.SevMedium || f.Basis != fact.Heuristic {
		t.Errorf("through an interface the claim is a heuristic: %+v", f)
	}
}

func TestStateWritesKnowWhetherTheTransactionIsOpen(t *testing.T) {
	m := machine(fixture(t, "paysvc").States(), ".PaymentStatus")
	inTx := map[string]bool{}
	for _, w := range m.Writes {
		if w.In.Symbol == "(*Server).process" {
			inTx[w.To] = w.InTx
		}
	}
	if inTx["StatusPending"] || !inTx["StatusAuthorized"] {
		t.Errorf("pending is set before BeginTx and authorized after it; testigo marked both as inside: %v", inTx)
	}
}

func stateLab(t *testing.T) *Program {
	return lab(t, "state", func() map[string]string {
		return map[string]string{
			"typedarg/domain/domain.go": `package domain

type Status string

const (
	StatusPending  Status = "pending"
	StatusCaptured Status = "captured"
)

type Payment struct{ Status Status }
`,
			"typedarg/repo/repo.go": `package repo

import (
	"context"
	"fmt"

	"example.com/m/typedarg/domain"
)

type Repo struct{}

func (Repo) UpdateStatus(ctx context.Context, id string, s domain.Status) error { return nil }

func Capture(ctx context.Context, r Repo, p *domain.Payment) error {
	p.Status = domain.StatusPending
	fmt.Println(domain.StatusPending)
	return r.UpdateStatus(ctx, "id", domain.StatusCaptured)
}
`,
			"guards/domain/domain.go": `package domain

import "errors"

type Status string

const (
	Pending    Status = "pending"
	Authorized Status = "authorized"
	Captured   Status = "captured"
	Failed     Status = "failed"
)

type Payment struct{ Status Status }

func (p *Payment) Authorize() {
	if p.Status == Pending {
		p.Status = Authorized
	}
}

func (p *Payment) Capture() error {
	if p.Status != Authorized {
		return errors.New("not authorized")
	}
	p.Status = Captured
	return nil
}

func (p *Payment) Fail() {
	switch p.Status {
	case Pending, Authorized:
		p.Status = Failed
	}
}

func (p *Payment) Reset(next Status) {
	prev := p.Status
	_ = prev
	p.Status = next
}
`,
		}
	})
}

func TestStateSetThroughATypedArgumentIsProduced(t *testing.T) {
	m := machine(stateLab(t).States(), "typedarg/domain.Status")
	if m == nil {
		t.Fatal("machine missing")
	}
	if slices.Contains(m.NeverAssigned, "StatusCaptured") {
		t.Fatal("StatusCaptured is written through UpdateStatus; testigo ignored call arguments and reported it as never set")
	}
	var via []fact.WriteVia
	for _, w := range m.Writes {
		via = append(via, w.Via)
	}
	if !slices.Equal(via, []fact.WriteVia{fact.ViaAssign, fact.ViaArgument}) {
		t.Errorf("writes via %v; printing a state is not producing it", via)
	}
}

func TestGuardsGiveTheSourceState(t *testing.T) {
	m := machine(stateLab(t).States(), "guards/domain.Status")
	if m == nil {
		t.Fatal("machine missing")
	}
	got := map[string][]string{}
	dynamic := 0
	for _, w := range m.Writes {
		if w.To == fact.DynamicState {
			dynamic++
			continue
		}
		got[w.To] = w.From
	}
	for to, from := range map[string][]string{
		"Authorized": {"Pending"},
		"Captured":   {"Authorized"},
		"Failed":     {"Authorized", "Pending"},
	} {
		if !slices.Equal(got[to], from) {
			t.Errorf("%s from %v, want %v", to, got[to], from)
		}
	}
	if dynamic != 1 {
		t.Errorf("dynamic writes = %d: p.Status = next is a transition, prev := p.Status is only a read", dynamic)
	}
	if len(m.Transitions()) != 4 {
		t.Errorf("transitions = %v", m.Transitions())
	}
}

func TestSQLAfterTheContextArgumentIsRead(t *testing.T) {
	p := fixture(t, "walletsvc")
	w := writesTo(p.TableWrites(), "wallets")
	if w == nil || w.Updates != 2 || w.Pattern != fact.WriteUpdateInPlace || !w.ReadModifyWrite {
		t.Fatalf("wallets = %+v: the balance is read, changed in Go and written back with ExecContext(ctx, sql, ...), which testigo never saw because it only read the first argument", w)
	}
	if tx := writesTo(p.TableWrites(), "transactions"); tx == nil || tx.Pattern != fact.WriteInsertOnly {
		t.Errorf("transactions = %+v", tx)
	}
}

func TestSQLInConstantsAndLocalsIsRead(t *testing.T) {
	p := repo(t, map[string]string{
		"migrations/1.sql": "CREATE TABLE accounts (id text PRIMARY KEY, balance bigint NOT NULL);",
		"svc/svc.go": `package svc

import (
	"context"
	"database/sql"
)

const debit = "UPDATE accounts SET balance = balance - $1 WHERE id = $2"

func Debit(ctx context.Context, db *sql.DB, id string, amount int64) error {
	_, err := db.ExecContext(ctx, debit, amount, id)
	return err
}

func Open(ctx context.Context, db *sql.DB, id string) error {
	q := "INSERT INTO accounts (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING"
	_, err := db.ExecContext(ctx, q, id)
	return err
}
`,
	})
	w := writesTo(p.TableWrites(), "accounts")
	if w == nil || !w.InDatabaseMath || !w.Upsert || w.Inserts != 1 || w.Updates != 1 {
		t.Fatalf("accounts = %+v", w)
	}
}

func TestIdempotencyKeyIsCheckedAgainstTheColumnItIsStoredIn(t *testing.T) {
	p := fixture(t, "paysvc")
	c, ok := p.IdempotencyKeys().Find("Payment", "IdempotencyKey")
	if !ok {
		t.Fatal("candidate missing")
	}
	if c.Table != "payments" || c.Column != "idem_key" {
		t.Errorf("mapped to %s.%s; the INSERT binds the field to idem_key, testigo assumed idempotency_key", c.Table, c.Column)
	}
	if !strings.Contains(strings.Join(c.Proof, ";"), "WHERE clause") {
		t.Errorf("alreadySeen selects by idem_key, so the key is looked up: %v", c.Proof)
	}
	fs := findings(p.Findings(), "IDEM-KEY-NOT-UNIQUE")
	if len(fs) != 1 || !strings.Contains(fs[0].Title, "IdempotencyKey") || !strings.Contains(fs[0].Detail, "payments.idem_key") {
		t.Errorf("findings = %+v; OrderID is not stored in payments, so it cannot lack a constraint there", fs)
	}
}

const keyed = `package domain

type Status string

const (
	Pending Status = "pending"
	Done    Status = "done"
)

type Payment struct {
	ID             string
	IdempotencyKey string
	Status         Status
}

func (p *Payment) Finish() { p.Status = Done }
func New() *Payment       { return &Payment{Status: Pending} }
`

const keyedAPI = `package api

import (
	"context"
	"database/sql"
	"net/http"

	"example.com/m/domain"
)

func Create(ctx context.Context, db *sql.DB, r *http.Request) error {
	p := domain.New()
	p.IdempotencyKey = r.Header.Get("Idempotency-Key")
	_, err := db.ExecContext(ctx, "INSERT INTO payments (id, idem_key) VALUES ($1, $2)", p.ID, p.IdempotencyKey)
	return err
}
`

func TestAUniqueIndexOnTheBoundColumnSatisfiesTheKey(t *testing.T) {
	p := repo(t, map[string]string{
		"domain/domain.go": keyed,
		"api/api.go":       keyedAPI,
		"migrations/1.sql": "CREATE TABLE payments (id text PRIMARY KEY, idem_key text NOT NULL UNIQUE);",
	})
	if fs := findings(p.Findings(), "IDEM-KEY-NOT-UNIQUE"); len(fs) != 0 {
		t.Fatalf("idem_key is unique at column level; testigo missed column-level UNIQUE and looked for idempotency_key: %+v", fs)
	}
	if c, ok := p.IdempotencyKeys().Decided(); !ok || c.Name != "IdempotencyKey" {
		t.Errorf("decided = %+v %v", c, ok)
	}
}

func TestACompositeUniqueScopedByTenantSatisfiesTheKey(t *testing.T) {
	p := repo(t, map[string]string{
		"domain/domain.go": keyed,
		"api/api.go":       keyedAPI,
		"migrations/1.sql": "CREATE TABLE payments (id text PRIMARY KEY, tenant text, idem_key text);\nCREATE UNIQUE INDEX p_tenant_key ON payments (tenant, idem_key);",
	})
	if fs := findings(p.Findings(), "IDEM-KEY-NOT-UNIQUE"); len(fs) != 0 {
		t.Fatalf("the key is unique per tenant; testigo only honoured a constraint the key leads: %+v", fs)
	}
	c, _ := p.IdempotencyKeys().Find("Payment", "IdempotencyKey")
	if !strings.Contains(strings.Join(c.Proof, ";"), "unique per tenant") {
		t.Errorf("proof = %v", c.Proof)
	}
}

func TestADroppedIndexNoLongerProtectsTheKey(t *testing.T) {
	p := repo(t, map[string]string{
		"domain/domain.go":        keyed,
		"api/api.go":              keyedAPI,
		"migrations/1_a.up.sql":   "CREATE TABLE payments (id text PRIMARY KEY, idem_key text);\nCREATE UNIQUE INDEX p_key ON payments (idem_key);",
		"migrations/1_a.down.sql": "DROP TABLE payments;",
		"migrations/2_b.up.sql":   "DROP INDEX p_key;",
		"migrations/2_b.down.sql": "CREATE UNIQUE INDEX p_key ON payments (idem_key);",
	})
	if len(findings(p.Findings(), "IDEM-KEY-NOT-UNIQUE")) != 1 {
		t.Fatal("migration 2 drops the index; testigo read every file flat, down files included, and still believed in it")
	}
	if infra := p.Infra(); infra.MigrationTool != "golang-migrate" || infra.MigrationsTotal != 2 || infra.MigrationsWithDown != 2 {
		t.Errorf("infra = %+v", infra)
	}
}

// modelLab holds scenarios about types and tags. None of them declares SQL
// migrations, so the schema is whatever the tags say.
func modelLab(t *testing.T) *Program {
	return lab(t, "model", func() map[string]string {
		return map[string]string{
			"origins/domain/domain.go": `package domain

type Status string

const Open Status = "open"

type Refund struct {
	ReferenceID string
	Status      Status
}

type Audit struct{ ReferenceID string }

func newID() string { return "x" }

func Log() Audit { return Audit{ReferenceID: newID()} }

func Start(ref string) *Refund { return &Refund{ReferenceID: ref, Status: Open} }
`,
			"origins/api/api.go": `package api

import (
	"net/http"

	"example.com/m/origins/domain"
)

func Handle(r *http.Request) {
	ref := r.Header.Get("X-Reference")
	_ = domain.Start(ref)
	refund := &domain.Refund{}
	refund.ReferenceID = r.Header.Get("X-Reference")
}
`,
			"quote/quote.go": `package quote

type Quote struct {
	GlobalWeight float64
	Syntax       float64
	FeeRate      float64
	Amount       float64
	Currency     string
}
`,
			"tags/tags.go": `package tags

type BaseModel struct{}

type Log struct {
	BaseModel ` + "`bun:\"table:logs,alias:l\"`" + `
	ID        int64  ` + "`bun:\"id,pk,autoincrement\"`" + `
	Ledger    string ` + "`bun:\"ledger,unique:ledger_key\"`" + `
	Key       string ` + "`bun:\"idempotency_key,unique:ledger_key\"`" + `
}

type Order struct {
	ID     string ` + "`gorm:\"primaryKey\"`" + `
	Shop   string ` + "`gorm:\"uniqueIndex:idx_shop_ref\"`" + `
	Ref    string ` + "`gorm:\"column:order_ref;uniqueIndex:idx_shop_ref\"`" + `
	Serial string ` + "`gorm:\"unique\"`" + `
}

func (Order) TableName() string { return "shop_orders" }
`,
			"pb/pay.pb.go": "// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage pb\n\ntype Charge struct{ Amount float64 }\n",
		}
	})
}

func TestOriginEvidenceBelongsToTheFieldNotTheName(t *testing.T) {
	p := modelLab(t)
	refund, ok := p.IdempotencyKeys().Find("Refund", "ReferenceID")
	if !ok {
		t.Fatal("Refund.ReferenceID missing")
	}
	for _, a := range refund.Against {
		if strings.Contains(a, "generated in this process") {
			t.Fatal("Audit.ReferenceID is generated; testigo pooled fields by name and disqualified Refund.ReferenceID too")
		}
	}
	if !strings.Contains(strings.Join(refund.Proof, ";"), "arrives from outside") {
		t.Errorf("proof = %v", refund.Proof)
	}
	audit, _ := p.IdempotencyKeys().Find("Audit", "ReferenceID")
	if !strings.Contains(strings.Join(audit.Against, ";"), "generated in this process") {
		t.Errorf("Audit.ReferenceID against = %v", audit.Against)
	}
}

func TestMoneyIsMatchedByWordsAndRatesAreNotAmounts(t *testing.T) {
	var flagged []string
	for _, f := range findings(modelLab(t).Findings(), "MONEY-FLOAT") {
		flagged = append(flagged, f.Title)
	}
	if len(flagged) != 1 || !strings.Contains(flagged[0], "Quote.Amount") {
		t.Fatalf("MONEY-FLOAT = %v; testigo's substring tables also flagged GlobalWeight (bal), Syntax (tax) and FeeRate", flagged)
	}
}

func TestBunAndGormTagsDescribeTheSchema(t *testing.T) {
	infra := modelLab(t).Infra()
	if cov, ok := infra.UniqueOn("logs", "idempotency_key"); !ok || !slices.Equal(cov.Scope, []string{"ledger"}) {
		t.Errorf("bun composite unique = %+v %v (testigo did not read bun tags at all)", cov, ok)
	}
	if cov, ok := infra.UniqueOn("shop_orders", "order_ref"); !ok || !slices.Equal(cov.Scope, []string{"shop"}) {
		t.Errorf("gorm uniqueIndex shared by two fields is one composite constraint: %+v %v", cov, ok)
	}
	if cov, ok := infra.UniqueOn("shop_orders", "serial"); !ok || len(cov.Scope) != 0 {
		t.Errorf("gorm unique = %+v %v", cov, ok)
	}
	if !infra.HasTable("shop_orders") || infra.HasTable("orders") {
		t.Errorf("TableName() overrides the plural; tables = %+v", infra.Tables)
	}
}

func TestGeneratedCodeIsNotReported(t *testing.T) {
	p := modelLab(t)
	for _, f := range p.Findings() {
		if f.Ref.File == "pb/pay.pb.go" {
			t.Fatalf("generated code is not the module's to fix: %+v", f)
		}
	}
	if !slices.Equal(p.GeneratedFiles(), []string{"pb/pay.pb.go"}) {
		t.Errorf("generated = %v", p.GeneratedFiles())
	}
}

func TestTestsForFindsTheTestsThatReachAFunction(t *testing.T) {
	p := repo(t, map[string]string{
		"pay/pay.go": `package pay

func fee(amount int64) int64 { return amount / 100 }

func Charge(amount int64) int64 { return amount + fee(amount) }

func Unused() {}
`,
		"pay/pay_test.go": `package pay

import "testing"

func TestCharge(t *testing.T) { Charge(100) }
`,
		"pay/ext_test.go": `package pay_test

import (
	"testing"

	"example.com/m/pay"
)

func TestFromOutside(t *testing.T) { pay.Charge(1) }
`,
	}, withTests)
	var got []string
	for _, l := range p.TestsFor("example.com/m/pay#fee", 0) {
		got = append(got, l.Test.Symbol+"@"+itoa(l.Hops))
	}
	if !slices.Equal(got, []string{"TestCharge@2", "TestFromOutside@2"}) {
		t.Fatalf("tests for fee = %v", got)
	}
	if links := p.TestsFor("example.com/m/pay#Unused", 0); len(links) != 0 {
		t.Errorf("nothing tests Unused: %+v", links)
	}
	if f, _ := p.Function("example.com/m/pay#fee"); f.InTest {
		t.Error("the production function comes from the plain package, not the test variant")
	}
}

func TestFunctionAtMapsALineToItsDeclaration(t *testing.T) {
	p := fixture(t, "paysvc")
	f, ok := p.FunctionAt("api/server.go", 64)
	if !ok || f.Ref.Symbol != "(*Server).process" {
		t.Fatalf("line 64 is inside process: %+v", f)
	}
	if _, ok := p.FunctionAt("api/server.go", 14); ok {
		t.Error("line 14 is a type declaration, not a function")
	}
}

func TestLoadErrorsAreFindingsWithALocation(t *testing.T) {
	p := repo(t, map[string]string{"pay/pay.go": "package pay\n\nfunc Charge() int { return \"x\" }\n"})
	fs := findings(p.Findings(), "LOAD-ERROR")
	if len(fs) != 1 || fs[0].Ref.File != "pay/pay.go" || fs[0].Line != 3 {
		t.Fatalf("LOAD-ERROR = %+v; testigo's had no file or line", fs)
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	encode := func() string {
		p, err := Load(t.Context(), Options{Dir: fixture(t, "walletsvc").Root()})
		if err != nil {
			t.Fatal(err)
		}
		flow, err := p.Flow([]fact.EntryPoint{
			{Pkg: "example.com/walletsvc/api", Symbol: "(*Server).Transfer"},
			{Pkg: "example.com/walletsvc/api", Symbol: "(*Server).Withdraw"},
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(struct {
			Flow     *fact.Flow
			Edges    []fact.Edge
			Findings []fact.Finding
		}{flow, p.Edges(), p.Findings()})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if a, b := encode(), encode(); a != b {
		t.Fatal("two loads of the same tree produced different output")
	}
}

func TestFlowNeedsEntryPoints(t *testing.T) {
	if _, err := fixture(t, "paysvc").Flow(nil); err != ErrNoEntries {
		t.Fatalf("err = %v", err)
	}
	if _, err := fixture(t, "paysvc").Flow([]fact.EntryPoint{{Pkg: "example.com/paysvc/api", Symbol: "Nope"}}); err == nil {
		t.Fatal("an unknown entry point must be an error")
	}
}
