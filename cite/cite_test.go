package cite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("api/server.go", "package api\n\nfunc (s *Server) process() error {\n\ttx, err := s.DB.BeginTx(ctx, nil)\n\treturn tx.Commit()\n}\n")
	write("migrations/0001.sql", "CREATE TABLE payments (id text);\n")
	return dir
}

func TestParseFindsCitationsInProse(t *testing.T) {
	got := Parse("the tx opens at api/server.go:4 and commits at api/server.go:5-6; schema in migrations/0001.sql:1.")
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got[1] != (Citation{File: "api/server.go", Line: 5, EndLine: 6}) {
		t.Errorf("range = %+v", got[1])
	}
	if !Has("see api/server.go:4") || Has("see migrations/0001.sql:1") || Has("server.go line 4") {
		t.Error("Has keeps testigo's rule: a Go file and a line number")
	}
}

func TestAWellFormedCitationOfANonexistentLineIsRejected(t *testing.T) {
	v, err := NewVerifier(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Check(Citation{File: "api/server.go", Line: 4}); err != nil {
		t.Fatalf("valid citation rejected: %v", err)
	}
	if err := v.Check(Citation{File: "api/server.go", Line: 40}); !errors.Is(err, ErrNoSuchLine) {
		t.Errorf("line 40 of a 6-line file: err = %v", err)
	}
	if err := v.Check(Citation{File: "api/handler.go", Line: 1}); !errors.Is(err, ErrNoSuchFile) {
		t.Errorf("invented file: err = %v", err)
	}
}

func TestPathsFromAModelCannotLeaveTheRepository(t *testing.T) {
	dir := repo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret.go", "/etc/passwd", "api/../../x.go", "link.go"} {
		if err := v.Check(Citation{File: p, Line: 1}); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%s: err = %v, want ErrOutsideRoot", p, err)
		}
	}
}

func TestQuoteMustBeNearTheCitedLine(t *testing.T) {
	v, err := NewVerifier(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Quote(Citation{File: "api/server.go", Line: 4}, "tx, err :=   s.DB.BeginTx(ctx, nil)"); err != nil {
		t.Errorf("whitespace differences must not fail a quote: %v", err)
	}
	if err := v.Quote(Citation{File: "api/server.go", Line: 5}, "s.DB.BeginTx"); err != nil {
		t.Errorf("off by one is within slack: %v", err)
	}
	if err := v.Quote(Citation{File: "api/server.go", Line: 4}, "defer tx.Rollback()"); !errors.Is(err, ErrNotQuoted) {
		t.Errorf("quote of code that is not there: err = %v", err)
	}
	if err := v.Quote(Citation{File: "api/server.go", Line: 4}, "   "); !errors.Is(err, ErrNotQuoted) {
		t.Errorf("an empty quote proves nothing: err = %v", err)
	}
}
