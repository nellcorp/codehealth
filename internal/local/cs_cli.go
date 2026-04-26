package local

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

// ErrCSNotFound indicates the CodeScene `cs` CLI is not on PATH.
var ErrCSNotFound = errors.New("local: codescene `cs` CLI not found on PATH")

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
	out, err := cmd.Output()
	if err != nil {
		// `cs check` exits non-zero on findings; capture stderr and try parsing.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			out = ee.Stderr
		}
		if len(out) == 0 {
			return nil, err
		}
	}

	var raw struct {
		Path       string `json:"path"`
		Health     float64 `json:"health"`
		Score      float64 `json:"score"`
		Findings   []struct {
			Function string `json:"function"`
			Line     int    `json:"line"`
			Code     string `json:"code"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
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
