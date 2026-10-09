package scan

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/katayunak/1819/gofacts/fact"
	"github.com/katayunak/1819/gofacts/internal/gomodtest"
)

var (
	fixtureMu   sync.Mutex
	fixtureProg = map[string]*Program{}
)

func fixture(t *testing.T, name string) *Program {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if p, ok := fixtureProg[name]; ok {
		return p
	}
	root, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(context.Background(), Options{Dir: root})
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	fixtureProg[name] = p
	return p
}

var paysvcEntries = []fact.EntryPoint{
	{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment"},
	{Pkg: "example.com/paysvc/webhook", Symbol: "(*Handler).PSPCallback"},
	{Pkg: "example.com/paysvc/recon", Symbol: "(*Job).Reconcile"},
}

func paysvcFlow(t *testing.T) *fact.Flow {
	t.Helper()
	f, err := fixture(t, "paysvc").Flow(paysvcEntries)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func repo(t *testing.T, files map[string]string, opts ...func(*Options)) *Program {
	t.Helper()
	o := Options{Dir: gomodtest.Write(t, files)}
	for _, f := range opts {
		f(&o)
	}
	p, err := Load(context.Background(), o)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return p
}

func withTests(o *Options) { o.Tests = true }

func findings(fs []fact.Finding, id string) []fact.Finding {
	var out []fact.Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func findingIn(fs []fact.Finding, id, symbol string) (fact.Finding, bool) {
	for _, f := range fs {
		if f.ID == id && strings.HasSuffix(f.Ref.ID(), "#"+symbol) {
			return f, true
		}
	}
	return fact.Finding{}, false
}

func machine(ms []fact.StateMachine, suffix string) *fact.StateMachine {
	for i := range ms {
		if strings.HasSuffix(ms[i].Type, suffix) {
			return &ms[i]
		}
	}
	return nil
}

func txIn(txs []fact.Tx, symbol string) *fact.Tx {
	for i := range txs {
		if txs[i].In.Symbol == symbol {
			return &txs[i]
		}
	}
	return nil
}

func writesTo(ws []fact.TableWrites, table string) *fact.TableWrites {
	for i := range ws {
		if ws[i].Table == table {
			return &ws[i]
		}
	}
	return nil
}

// fakeSQLDriverModule is a stand-in for a database library at a real import
// path, wired by a replace directive so tests stay offline. Its methods reach
// database/sql, which is what makes the seam filters accept them.
const fakeGorm = `package gorm

import "database/sql"

type DB struct {
	Error error
	sql   *sql.DB
}

func (db *DB) Begin() *DB { return db }
func (db *DB) Commit() *DB { return db }
func (db *DB) Rollback() *DB { return db }
func (db *DB) Create(v any) *DB { db.sql.Exec("insert into x values (1)"); return db }
func (db *DB) Transaction(fc func(tx *DB) error) error { return fc(db) }
`

func withFakeGorm(files map[string]string) map[string]string {
	files["go.mod"] = "module example.com/m\n\ngo 1.22\n\nrequire gorm.io/gorm v0.0.0\n\nreplace gorm.io/gorm => ./thirdparty/gorm\n"
	files["thirdparty/gorm/go.mod"] = "module gorm.io/gorm\n\ngo 1.22\n"
	files["thirdparty/gorm/gorm.go"] = fakeGorm
	return files
}

var (
	labMu sync.Mutex
	labs  = map[string]*Program{}
)

// lab loads a module shared by several tests, once. Scenarios that do not
// depend on module-wide state (the schema) live side by side in one, each in
// its own file or package, because a load is the expensive part of a test.
func lab(t *testing.T, name string, files func() map[string]string, opts ...func(*Options)) *Program {
	t.Helper()
	labMu.Lock()
	defer labMu.Unlock()
	if p, ok := labs[name]; ok {
		return p
	}
	p := repo(t, files(), opts...)
	labs[name] = p
	return p
}
