package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Captured from a real `cs review --output-format json --pretty <file>`
// run against a complex Go file. Used as the source of truth for the
// parser shape — when this stops matching the binary, update both
// together.
const realCSReviewSample = `{
  "score": 9.6,
  "review": [{
    "category": "Complex Method",
    "functions": [{
      "title": "Big",
      "details": "cc = 21",
      "start-line": 3,
      "end-line": 25,
      "url": ""
    }],
    "description": "A Complex Method has a high cyclomatic complexity.",
    "indication": 2
  }]
}`

func TestParseCSCheckRealShape(t *testing.T) {
	got, err := parseCSCheck([]byte(realCSReviewSample), "big.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 9.6 {
		t.Fatalf("health: want 9.6, got %v", got.Health)
	}
	if len(got.Smells) != 1 {
		t.Fatalf("want 1 smell, got %d (%+v)", len(got.Smells), got.Smells)
	}
	s := got.Smells[0]
	if s.Function != "Big" || s.Line != 3 || s.Kind != "complexity" || s.Message != "cc = 21" {
		t.Fatalf("smell mapping wrong: %+v", s)
	}
}

func TestParseCSCheckCleanFile(t *testing.T) {
	// Real shape when cs finds nothing: `score=10.0, review=[]`.
	got, err := parseCSCheck([]byte(`{"score": 10.0, "review": []}`), "x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 10 {
		t.Fatalf("clean file: want 10, got %v", got.Health)
	}
	if len(got.Smells) != 0 {
		t.Fatalf("clean file: want 0 smells, got %+v", got.Smells)
	}
}

func TestParseCSCheckNonJSONDegrades(t *testing.T) {
	// `cs review` may emit plain text (e.g. "Invalid path: ...") for
	// files it doesn't recognise. Parser must degrade to a neutral
	// score so the pre-commit hook doesn't crash.
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
	if !strings.Contains(warned, "no JSON") || !strings.Contains(warned, "foo.go") {
		t.Fatalf("warning should mention no JSON and the path; got %q", warned)
	}
}

func TestParseCSCheckMalformedJSONErrors(t *testing.T) {
	// Body that *looks* like JSON but isn't valid → propagate the error.
	// Strict callers rely on this so cs output corruption fails loudly
	// rather than passing every commit with Health=10.
	prev := WarnUnparsableCSTo
	defer func() { WarnUnparsableCSTo = prev }()
	WarnUnparsableCSTo = func(string) { t.Fatal("should not warn on malformed JSON") }

	_, err := parseCSCheck([]byte(`{"score": "not-a-number"`), "x.go")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Fatalf("error should mention parse JSON; got %v", err)
	}
}

func TestParseCSCheckSkipsPreambleLogs(t *testing.T) {
	// cs sometimes prepends warning lines (e.g. about git context) to
	// the JSON body. Parser jumps to the first `{`.
	in := []byte("git execution failed: fatal: not a git repository\n{\"score\": 9.0, \"review\": []}")
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

func TestParseCSCheckSchemaDriftWarns(t *testing.T) {
	// cs returns valid JSON but uses entirely different top-level keys.
	// Parser must warn so users notice the cs version drifted rather
	// than getting a silent Health=10 every commit.
	var warned string
	prev := WarnUnparsableCSTo
	defer func() { WarnUnparsableCSTo = prev }()
	WarnUnparsableCSTo = func(msg string) { warned = msg }

	in := []byte(`{"file":"x.go","quality":7.0,"issues":[{"name":"complex"}]}`)
	got, err := parseCSCheck(in, "x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Health != 10 {
		t.Fatalf("with no recognised keys, health falls back to 10; got %v", got.Health)
	}
	if !strings.Contains(warned, "none of") || !strings.Contains(warned, "x.go") {
		t.Fatalf("warning should mention drift and path; got %q", warned)
	}
	if !strings.Contains(warned, "file") || !strings.Contains(warned, "quality") {
		t.Fatalf("warning should list observed keys; got %q", warned)
	}
}

func TestBridgeCSToken(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		wantHas string // expected CS_ACCESS_TOKEN value after bridge ("" = absent)
	}{
		{
			name:    "bridges CODESCENE_TOKEN when CS_ACCESS_TOKEN unset",
			in:      []string{"PATH=/usr/bin", "CODESCENE_TOKEN=pat-codescene"},
			wantHas: "pat-codescene",
		},
		{
			name:    "respects explicit CS_ACCESS_TOKEN",
			in:      []string{"CS_ACCESS_TOKEN=pat-cs", "CODESCENE_TOKEN=pat-codescene"},
			wantHas: "pat-cs",
		},
		{
			name:    "treats empty CS_ACCESS_TOKEN as unset",
			in:      []string{"CS_ACCESS_TOKEN=", "CODESCENE_TOKEN=pat-codescene"},
			wantHas: "pat-codescene",
		},
		{
			name:    "no-op when neither var set",
			in:      []string{"PATH=/usr/bin"},
			wantHas: "",
		},
		{
			name:    "no-op when only CS_ACCESS_TOKEN set",
			in:      []string{"CS_ACCESS_TOKEN=pat-cs"},
			wantHas: "pat-cs",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bridgeCSToken(c.in)
			// Take the *last* CS_ACCESS_TOKEN occurrence — that's the
			// effective value Linux exec uses when env has duplicates.
			var effective string
			for _, kv := range got {
				if strings.HasPrefix(kv, "CS_ACCESS_TOKEN=") {
					effective = kv[len("CS_ACCESS_TOKEN="):]
				}
			}
			if effective != c.wantHas {
				t.Errorf("CS_ACCESS_TOKEN: got %q, want %q (env=%v)", effective, c.wantHas, got)
			}
		})
	}
}

func TestLooksUnauthenticated(t *testing.T) {
	cases := map[string]bool{
		"In order to use the full CLI tool you will need to set up your environment with a Personal Access Token.": true,
		"export CS_ACCESS_TOKEN=<your-PAT>": true,
		`{"score":9.5, "review":[]}`:        false,
		"":                                  false,
		"Invalid path: foo.go":              false,
	}
	for in, want := range cases {
		if got := looksUnauthenticated([]byte(in)); got != want {
			t.Errorf("looksUnauthenticated(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNormaliseKindCSCategories(t *testing.T) {
	// cs uses TitleCase category names like "Complex Method", "Bumpy
	// Road Ahead", "Code Duplication" — make sure mapping is case
	// insensitive so the codehealth Kind taxonomy stays stable.
	cases := map[string]string{
		"Complex Method":    "complexity",
		"Deep Nested Logic": "deep_nesting",
		"Long Method":       "long_function",
		"Bumpy Road Ahead":  "bumpy_road",
		"Code Duplication":  "duplication",
		"":                  "smell",
		"Unknown Category":  "Unknown Category",
	}
	for in, want := range cases {
		if got := normaliseKind(in); got != want {
			t.Errorf("normaliseKind(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScoreFileCSIntegration shells out to the real `cs` binary when
// one is on PATH. Catches the invocation/auth/schema mistakes parser
// unit tests miss. Skipped automatically when `cs` is absent.
//
// Behaviour depends on whether CS_ACCESS_TOKEN is set:
//   - Unset: assert ErrCSNotAuthenticated.
//   - Set:   assert real scoring works and the schema-drift warning
//     does NOT fire (which would mean the parser fell out of sync).
func TestScoreFileCSIntegration(t *testing.T) {
	if !HasCSCLI("cs") {
		t.Skip("cs CLI not on PATH; skipping integration test")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "x.go")
	if err := os.WriteFile(src, []byte("package x\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	csToken := os.Getenv("CS_ACCESS_TOKEN")
	codesceneToken := os.Getenv("CODESCENE_TOKEN")
	if csToken == "" && codesceneToken == "" {
		_, err := ScoreFileCS(context.Background(), "cs", src)
		if !errors.Is(err, ErrCSNotAuthenticated) {
			t.Fatalf("want ErrCSNotAuthenticated, got %v", err)
		}
		return
	}

	// If only CODESCENE_TOKEN is set, the bridge in ScoreFileCS must
	// export it as CS_ACCESS_TOKEN to the subprocess. Clear the cs var
	// for the duration of this test to exercise that path explicitly.
	if csToken == "" && codesceneToken != "" {
		t.Setenv("CS_ACCESS_TOKEN", "")
	}

	var warned string
	prev := WarnUnparsableCSTo
	defer func() { WarnUnparsableCSTo = prev }()
	WarnUnparsableCSTo = func(msg string) { warned = msg }

	got, err := ScoreFileCS(context.Background(), "cs", src)
	if err != nil {
		t.Fatalf("ScoreFileCS with token: %v", err)
	}
	if got == nil || got.Backend != "cs" {
		t.Fatalf("want cs-backed score, got %+v", got)
	}
	if warned != "" {
		t.Fatalf("schema drift warning fired — parser fields likely need updating: %s", warned)
	}
	if got.Health <= 0 {
		t.Fatalf("trivial file should score > 0; got %v (schema drift?)", got.Health)
	}
}
