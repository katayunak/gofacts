package fact

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestTenantSchemeFindsAColumnThatLeadsUniquenessOnTwoTables(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "idempotency_key"}, Kind: KindUniqueIndex},
		{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: KindUniqueIndex},
	}}
	got, ok := i.TenantScheme()
	if !ok || got.Column != "ledger" || !slices.Equal(got.Tables, []string{"logs", "transactions"}) {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestTenantSchemeNeedsCompositeUniquenessOnTwoTables(t *testing.T) {
	for name, cons := range map[string][]Constraint{
		"one table": {{Table: "logs", Columns: []string{"ledger", "k"}, Kind: KindUniqueIndex}},
		"single column": {
			{Table: "users", Columns: []string{"email"}, Kind: KindUniqueIndex},
			{Table: "accounts", Columns: []string{"email"}, Kind: KindUniqueIndex},
		},
		"primary keys": {
			{Table: "logs", Columns: []string{"ledger", "id"}, Kind: KindPrimaryKey},
			{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: KindPrimaryKey},
		},
	} {
		if _, ok := (Infra{Constraints: cons}).TenantScheme(); ok {
			t.Errorf("%s: not a tenant scheme", name)
		}
	}
}

func TestTenantSchemePicksTheColumnWithMoreTables(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "k"}, Kind: KindUniqueIndex},
		{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: KindUniqueIndex},
		{Table: "widgets", Columns: []string{"shop_id", "sku"}, Kind: KindUniqueConstraint},
		{Table: "orders", Columns: []string{"shop_id", "order_id"}, Kind: KindUniqueConstraint},
		{Table: "carts", Columns: []string{"shop_id", "cart_id"}, Kind: KindUniqueConstraint},
	}}
	if got, _ := i.TenantScheme(); got.Column != "shop_id" {
		t.Fatalf("got %q", got.Column)
	}
}

func TestUniqueOnPrefersTheStrongestGuarantee(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "p", Columns: []string{"tenant", "key"}, Kind: KindUniqueIndex},
		{Table: "p", Columns: []string{"key"}, Kind: KindUniqueIndex, Where: "deleted_at IS NULL"},
		{Table: "other", Columns: []string{"key"}, Kind: KindUniqueIndex},
	}}
	cov, ok := i.UniqueOn("p", "key")
	if !ok || cov.Partial || !slices.Equal(cov.Scope, []string{"tenant"}) {
		t.Fatalf("a full composite constraint beats a partial one: %+v", cov)
	}
	if _, ok := i.UniqueOn("p", "missing"); ok {
		t.Error("no constraint covers missing")
	}
	if _, ok := (Infra{Constraints: []Constraint{{Table: "p", Columns: []string{"key"}, Kind: "check"}}}).UniqueOn("p", "key"); ok {
		t.Error("only uniqueness kinds count")
	}
}

func TestCandidatesDecideOnlyOnAClearLead(t *testing.T) {
	strong := Candidate{Name: "IdempotencyKey", Score: 7, Declarative: true}
	cases := []struct {
		name string
		cs   Candidates
		want bool
	}{
		{"clear winner", Candidates{strong, {Name: "OrderID", Score: 4, Declarative: true}}, true},
		{"near tie", Candidates{strong, {Name: "OrderID", Score: 5, Declarative: true}}, false},
		{"name only", Candidates{{Name: "Ref", Score: 1, Declarative: true}}, false},
		{"not declarative", Candidates{{Name: "X", Score: 9}}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		if _, ok := c.cs.Decided(); ok != c.want {
			t.Errorf("%s: decided=%v, want %v", c.name, ok, c.want)
		}
	}
}

func TestWeakestCertaintyIsThePathsCertainty(t *testing.T) {
	if Weakest(Certain, Likely) != Likely || Weakest(Possible, Certain) != Possible || Weakest(Certain, Certain) != Certain {
		t.Fatal("a call chain is only as certain as its least certain edge")
	}
}

func TestFindingsSortBySeverityThenRuleThenPlace(t *testing.T) {
	fs := []Finding{
		{ID: "B", Severity: SevMedium, Ref: CodeRef{File: "a.go"}, Line: 1},
		{ID: "A", Severity: SevCritical, Ref: CodeRef{File: "b.go"}, Line: 9},
		{ID: "A", Severity: SevCritical, Ref: CodeRef{File: "b.go"}, Line: 2},
		{ID: "Z", Severity: SevInfo},
		{ID: "C", Severity: SevLow},
	}
	SortFindings(fs)
	var got []string
	for _, f := range fs {
		got = append(got, f.ID+":"+string(f.Severity))
	}
	want := []string{"A:critical", "A:critical", "B:medium", "C:low", "Z:info"}
	if !slices.Equal(got, want) || fs[0].Line != 2 {
		t.Fatalf("order = %v", got)
	}
}

func TestFunctionContainsItsLines(t *testing.T) {
	f := Function{Ref: CodeRef{File: "a.go", Line: 10}, EndLine: 20}
	if !f.Contains("a.go", 10) || !f.Contains("a.go", 20) || f.Contains("a.go", 21) || f.Contains("b.go", 15) {
		t.Fatal("Contains")
	}
}

// testigo reads flow.json written by older versions; every version 4 key
// must keep its name.
func TestFlowKeepsTestigoVersion4Keys(t *testing.T) {
	flow := NewFlow("example.com/m")
	flow.Nodes["x#F"] = &Node{Ref: CodeRef{Pkg: "x", Symbol: "F"}, Position: NodePositionEntry}
	flow.Findings = []Finding{{ID: "TX-NET-CALL", Severity: SevCritical}}
	flow.Seams = []Seam{{Kind: SeamDB}}
	b, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "module", "entries", "nodes", "seams", "states", "findings"} {
		if _, ok := top[key]; !ok {
			t.Errorf("flow.json lost key %q", key)
		}
	}
	var nodes map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["nodes"], &nodes); err != nil {
		t.Fatal(err)
	}
	node := nodes["x#F"]
	for _, key := range []string{"ref", "position", "facts"} {
		if _, ok := node[key]; !ok {
			t.Errorf("node lost key %q", key)
		}
	}
}
