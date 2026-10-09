package goenv

import (
	"os"
	"path/filepath"
	"testing"
)

func module(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range append([]string{"go.mod"}, files...) {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("module example.com/m\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInheritedFlagsCannotReachTheGoCommand(t *testing.T) {
	dir := module(t)
	base := []string{
		"HOME=/home/x",
		"GOFLAGS=-mod=vendor -toolexec=/tmp/evil",
		"GOTOOLCHAIN=go1.99.0",
		"CGO_ENABLED=1",
		"GOWORK=/somewhere/go.work",
	}
	env := For(dir, base, Options{})

	for key, want := range map[string]string{
		"GOFLAGS":     "-mod=readonly",
		"GOTOOLCHAIN": "local",
		"CGO_ENABLED": "0",
		"GOWORK":      "off",
		"HOME":        "/home/x",
	} {
		if got, _ := Get(env, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	for _, kv := range env {
		if kv == "GOFLAGS=-mod=vendor -toolexec=/tmp/evil" {
			t.Fatal("the inherited GOFLAGS survived; -toolexec would run an arbitrary program on every compile")
		}
	}
}

func TestVendoredRepositoryUsesItsVendorDirectory(t *testing.T) {
	dir := module(t, "vendor/modules.txt")
	if got := ModFlag(filepath.Join(dir)); got != "-mod=vendor" {
		t.Fatalf("got %q, want -mod=vendor", got)
	}
	sub := filepath.Join(dir, "internal", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ModFlag(sub); got != "-mod=vendor" {
		t.Fatalf("a subdirectory of a vendored module got %q", got)
	}
}

func TestCommittedWorkspaceIsHonoured(t *testing.T) {
	dir := module(t, "go.work")
	if got := Workspace(dir); got != filepath.Join(dir, "go.work") {
		t.Fatalf("got %q", got)
	}
}

func TestOfflineDisablesTheProxyAndExtraCannotOverride(t *testing.T) {
	env := For(module(t), []string{"GOPROXY=https://proxy.golang.org"}, Options{
		Offline: true,
		Extra:   []string{"GOFLAGS=-toolexec=x", "GOPRIVATE=example.com"},
	})
	if got, _ := Get(env, "GOPROXY"); got != "off" {
		t.Errorf("GOPROXY = %q, want off", got)
	}
	if got, _ := Get(env, "GOFLAGS"); got != "-mod=readonly" {
		t.Errorf("Extra overrode GOFLAGS: %q", got)
	}
	if got, _ := Get(env, "GOPRIVATE"); got != "example.com" {
		t.Errorf("GOPRIVATE = %q", got)
	}
}
