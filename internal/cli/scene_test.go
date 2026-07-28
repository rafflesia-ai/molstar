package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafflesia-ai/molstar/internal/render"
)

// The MVS validator explains exactly what is wrong, but only on stderr. The
// JSON envelope carried a bare "exit status 1", so a caller reading --json —
// the documented way — got an exit code and nothing else.
func TestSceneValidateSurfacesTheValidatorDiagnosis(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct{ body, want string }{
		"bad root kind": {`{"metadata":{"version":"1"},"root":{"kind":"bogus"}}`, "root"},
		"missing root":  {`{"metadata":{"version":"1"}}`, "root"},
		"not json":      {`hello`, "SyntaxError"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".mvsj")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, _, err := runAppForTest(context.Background(), "scene", "validate", path, "--json")
			if err == nil {
				t.Fatalf("%s should fail validation\n%s", name, stdout)
			}
			message := err.Error()
			if !strings.Contains(message, tc.want) {
				t.Fatalf("message should carry the validator's diagnosis (%q): %s", tc.want, message)
			}
			if message == "exit status 1" {
				t.Fatalf("message is still just the exit status: %s", message)
			}
		})
	}
}

// withCommandDetail must not invent detail when there is none, and must leave a
// nil error alone.
func TestWithCommandDetailIsConservative(t *testing.T) {
	base := errors.New("exit status 1")
	if got := withCommandDetail(base, render.CommandResult{}); got != base {
		t.Fatalf("no output should leave the error untouched, got %v", got)
	}
	if withCommandDetail(nil, render.CommandResult{Stderr: "boom"}) != nil {
		t.Fatal("nil should stay nil")
	}
	got := withCommandDetail(base, render.CommandResult{Stderr: "\n\nInvalid root node kind\n"})
	if !strings.Contains(got.Error(), "Invalid root node kind") {
		t.Fatalf("stderr detail should be folded in: %v", got)
	}
	if !errors.Is(got, base) {
		t.Fatal("the original error must stay wrapped")
	}
	// stdout is used when stderr is empty.
	got = withCommandDetail(base, render.CommandResult{Stdout: "problem on stdout"})
	if !strings.Contains(got.Error(), "problem on stdout") {
		t.Fatalf("stdout should be used as a fallback: %v", got)
	}
}
