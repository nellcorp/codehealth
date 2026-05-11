package local

import (
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
