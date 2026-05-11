package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrCSNotFound indicates the CodeScene `cs` CLI is not on PATH.
var ErrCSNotFound = errors.New("local: codescene `cs` CLI not found on PATH")

// WarnUnparsableCSTo is the sink used when `cs check --json` emits
// non-JSON output (e.g. `Invalid path: ...`) instead of the documented
// schema. Overridable in tests; wired to stderr in main.
var WarnUnparsableCSTo = func(msg string) { _ = msg }

// HasCSCLI reports whether the configured `cs` binary is invokable.
func HasCSCLI(bin string) bool {
	if bin == "" {
		bin = "cs"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// ScoreFileCS scores one file by shelling out to `cs check --json <path>`.
//
// `cs check` is CodeScene's local lint-like command. The exact JSON shape
// is vendor-specific; we read the conventional top-level fields and
// degrade to "unknown but no error" if the schema differs. When the
// binary is missing or the call fails, ErrCSNotFound is returned so the
// caller can fall back to the pure-Go backend.
func ScoreFileCS(ctx context.Context, bin, path string) (*Score, error) {
	if bin == "" {
		bin = "cs"
	}
	if !HasCSCLI(bin) {
		return nil, ErrCSNotFound
	}
	cmd := exec.CommandContext(ctx, bin, "check", "--json", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// `cs check` exits non-zero on findings; prefer stdout for the JSON
	// body and fall back to stderr only if stdout is empty. This keeps
	// findings intact when the CLI signals "issues present" via exit code.
	out := stdout.Bytes()
	if len(out) == 0 {
		out = stderr.Bytes()
	}
	if len(out) == 0 {
		if runErr != nil {
			return nil, runErr
		}
		return &Score{Path: path, Health: 10, Backend: "cs"}, nil
	}
	return parseCSCheck(out, path)
}

// parseCSCheck parses one cs-check JSON document. When the body is not
// JSON (e.g. cs emitted "Invalid path: ..." for files not yet known to
// the project), it degrades to a neutral score (Health=10, no smells)
// and emits a one-line warning via WarnUnparsableCSTo. This matches the
// "unknown but no error" contract documented on ScoreFileCS and stops
// the pre-commit hook from crashing on routine cs quirks.
func parseCSCheck(out []byte, path string) (*Score, error) {
	body := bytes.TrimSpace(out)
	// Some cs versions prepend log lines to the JSON body; jump to the
	// first object marker before parsing.
	if i := bytes.IndexByte(body, '{'); i > 0 {
		body = body[i:]
	}
	var raw struct {
		Path     string  `json:"path"`
		Health   float64 `json:"health"`
		Score    float64 `json:"score"`
		Findings []struct {
			Function string `json:"function"`
			Line     int    `json:"line"`
			Code     string `json:"code"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		WarnUnparsableCSTo(fmt.Sprintf(
			"codehealth: cs check produced non-JSON for %q (%v); degrading to neutral score. Raw: %s",
			path, err, truncate(out, 200)))
		return &Score{Path: path, Health: 10, Backend: "cs"}, nil
	}

	health := raw.Health
	if health == 0 && raw.Score > 0 {
		health = raw.Score
	}
	if health == 0 {
		health = 10
	}

	smells := make([]Smell, 0, len(raw.Findings))
	for _, f := range raw.Findings {
		smells = append(smells, Smell{
			Function: f.Function,
			Line:     f.Line,
			Kind:     normaliseKind(f.Code),
			Message:  strings.TrimSpace(f.Message),
		})
	}
	return &Score{
		Path:    path,
		Health:  health,
		Backend: "cs",
		Smells:  smells,
	}, nil
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func normaliseKind(code string) string {
	switch {
	case strings.Contains(code, "complex"):
		return "complexity"
	case strings.Contains(code, "nest"):
		return "deep_nesting"
	case strings.Contains(code, "long"):
		return "long_function"
	default:
		if code != "" {
			return code
		}
		return "smell"
	}
}
