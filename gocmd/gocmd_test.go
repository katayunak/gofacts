package gocmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/katayunak/1819/gofacts/internal/gomodtest"
)

func TestAFailingTestIsAResultNotAnError(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestFails(t *testing.T) { t.Fatal(\"boom\") }\n",
	})
	res, err := Run(context.Background(), dir, Options{}, "test", "./...")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.OK() || res.ExitCode == 0 || !strings.Contains(res.Output, "boom") {
		t.Fatalf("res = %+v", res)
	}
}

func TestHostileGOFLAGSDoNotReachTheBuild(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=vendor")
	dir := gomodtest.Write(t, map[string]string{"a/a.go": "package a\n"})
	res, err := Run(context.Background(), dir, Options{}, "build", "./...")
	if err != nil || !res.OK() {
		t.Fatalf("build of a non-vendored module under GOFLAGS=-mod=vendor: %+v %v", res, err)
	}
}

func TestTheDeadlineKillsTheTestBinary(t *testing.T) {
	dir := gomodtest.Write(t, map[string]string{
		"a/a_test.go": "package a\n\nimport (\"testing\"; \"time\")\n\nfunc TestHangs(t *testing.T) { time.Sleep(time.Hour) }\n",
	})
	start := time.Now()
	res, err := Run(context.Background(), dir, Options{Timeout: 8 * time.Second}, "test", "-count=1", "./...")
	if !errors.Is(err, ErrTimeout) || !res.TimedOut {
		t.Fatalf("err = %v res = %+v", err, res)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("returned after %s: the hung test binary outlived the deadline", elapsed)
	}
}

func TestBufferKeepsHeadAndTail(t *testing.T) {
	b := newBoundedBuffer(10)
	b.Write([]byte("HEAD-"))
	b.Write([]byte(strings.Repeat("x", 100)))
	b.Write([]byte("-TAIL"))
	got := b.String()
	if !b.Truncated() || !strings.HasPrefix(got, "HEAD-") || !strings.HasSuffix(got, "-TAIL") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "100 bytes omitted") {
		t.Errorf("omission not reported: %q", got)
	}
}
