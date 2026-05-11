package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCSCheckValid(t *testing.T) {
	in := []byte(`{"path":"x.go","health":7.5,"findings":[{"function":"Foo","line":10,"code":"complex-method","message":"Cyclomatic 15","severity":"warning"}]}`)
	got, err := parseCSCheck(in, "x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 7.5 {
		t.Fatalf("health: want 7.5, got %v", got.Health)
	}
	if len(got.Smells) != 1 || got.Smells[0].Kind != "complexity" {
		t.Fatalf("smells: got %+v", got.Smells)
	}
}

func TestParseCSCheckNonJSONDegrades(t *testing.T) {
	// Reproduces the pre-commit failure: `cs check --json` emits
	// `Invalid path: ...` instead of JSON. Parser must degrade to a
	// neutral score so the hook doesn't crash.
	var warned string
	prev := WarnUnparsableCSTo
	defer func() { WarnUnparsableCSTo = prev }()
	WarnUnparsableCSTo = func(msg string) { warned = msg }

	got, err := parseCSCheck([]byte("Invalid path: foo.go\n"), "foo.go")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got == nil || got.Health != 10 || got.Backend != "cs" {
		t.Fatalf("want neutral cs score, got %+v", got)
	}
	if !strings.Contains(warned, "non-JSON") || !strings.Contains(warned, "foo.go") {
		t.Fatalf("warning should mention non-JSON and the path; got %q", warned)
	}
}

func TestParseCSCheckSkipsPreambleLogs(t *testing.T) {
	// Some cs versions print log lines before the JSON object.
	in := []byte("Loading project...\nReady.\n{\"health\":9.0}")
	got, err := parseCSCheck(in, "x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 9.0 {
		t.Fatalf("health: want 9.0, got %v", got.Health)
	}
}

func TestParseCSCheckEmptyBodyDegrades(t *testing.T) {
	got, err := parseCSCheck([]byte("   \n"), "x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 10 {
		t.Fatalf("want neutral score on empty body, got %+v", got)
	}
}

func TestLooksUnauthenticated(t *testing.T) {
	cases := map[string]bool{
		"In order to use the full CLI tool you will need to set up your environment with a Personal Access Token.": true,
		"export CS_ACCESS_TOKEN=<your-PAT>": true,
		`{"health":9.5}`:                    false,
		"":                                  false,
		"Invalid path: foo.go":              false,
	}
	for in, want := range cases {
		if got := looksUnauthenticated([]byte(in)); got != want {
			t.Errorf("looksUnauthenticated(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestScoreFileCSIntegration shells out to the real `cs` binary when
// one is on PATH. It is the integration check that the unit tests
// above can't provide. Skipped automatically in environments without
// `cs` (most CI runners).
func TestScoreFileCSIntegration(t *testing.T) {
	if !HasCSCLI("cs") {
		t.Skip("cs CLI not on PATH; skipping integration test")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "x.go")
	if err := os.WriteFile(src, []byte("package x\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop CS_ACCESS_TOKEN to verify unauthenticated detection — this
	// is the failure mode users hit when running `--strict` without
	// setting the env var.
	t.Setenv("CS_ACCESS_TOKEN", "")
	_, err := ScoreFileCS(context.Background(), "cs", src)
	if !errors.Is(err, ErrCSNotAuthenticated) {
		t.Fatalf("want ErrCSNotAuthenticated, got %v", err)
	}
}
