package load

import (
	"context"
	"strings"
	"testing"

	"github.com/katayunak/1819/gofacts/internal/gomodtest"
)

func TestLoadsDespiteAHostileGlobalGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=vendor")
	t.Setenv("GOTOOLCHAIN", "go1.99.0")
	dir := gomodtest.Write(t, map[string]string{
		"pay/pay.go": "package pay\n\nfunc Charge() int { return 1 }\n",
	})

	res, err := Packages(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("a repository without vendor/ must load even when the caller exports GOFLAGS=-mod=vendor: %v", err)
	}
	if res.Module != "example.com/m" {
		t.Errorf("module = %q", res.Module)
	}
	if len(res.Issues) != 0 {
		t.Errorf("unexpected issues: %+v", res.Issues)
	}
}

func TestTypeErrorsBecomeLocatedIssues(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{
		"pay/pay.go": "package pay\n\nfunc Charge() int {\n\treturn \"one\"\n}\n",
	})

	res, err := Packages(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("a type error is a fact about the repository, not a reason to fail: %v", err)
	}
	if len(res.Issues) != 1 {
		t.Fatalf("issues = %+v, want one", res.Issues)
	}
	is := res.Issues[0]
	if is.Kind != "type" || is.Pkg != "example.com/m/pay" {
		t.Errorf("issue = %+v", is)
	}
	if is.Pos.File != "pay/pay.go" || is.Pos.Line != 4 {
		t.Errorf("position %+v, want pay/pay.go:4 repo-relative", is.Pos)
	}
}

func TestCancelledContextStopsTheLoad(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{"a/a.go": "package a\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Packages(ctx, Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
	}
}

func TestTestsAreLoadedWithoutTheGeneratedMain(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{
		"a/a.go":      "package a\n\nfunc F() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { F() }\n",
	})
	res, err := Packages(context.Background(), Config{Dir: dir, Tests: true})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range res.Packages {
		ids = append(ids, p.ID)
		if strings.HasSuffix(p.ID, ".test") {
			t.Errorf("generated test main %s should be dropped", p.ID)
		}
	}
	if len(ids) != 2 {
		t.Errorf("packages = %v, want the package and its test variant", ids)
	}
}

func TestNoPackagesIsAnError(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{"README.md": "nothing here"})
	if _, err := Packages(context.Background(), Config{Dir: dir}); err == nil {
		t.Fatal("a directory with no Go packages must be reported, not scanned as empty")
	}
}

func TestRelNeverClaimsAPathOutsideRoot(t *testing.T) {
	for _, c := range []struct{ root, path, want string }{
		{"/r", "/r/a/b.go", "a/b.go"},
		{"/r", "/other/x.go", "/other/x.go"},
		{"/r", "/r/../r2/x.go", "/r/../r2/x.go"},
		{"/r", "/r/..x/y.go", "..x/y.go"},
	} {
		if got := Rel(c.root, c.path); got != c.want {
			t.Errorf("Rel(%q,%q) = %q, want %q", c.root, c.path, got, c.want)
		}
	}
}
