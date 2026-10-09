// Package gomodtest writes throwaway Go modules for tests.
package gomodtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Write creates files under a fresh temporary directory and returns it. A
// go.mod is added when files does not contain one.
func Write(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/m\n\ngo 1.22\n"
	}
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
