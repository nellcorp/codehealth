package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ErrCSNotFound indicates the CodeScene `cs` CLI is not on PATH.
var ErrCSNotFound = errors.New("local: codescene `cs` CLI not found on PATH")

// ErrCSNotAuthenticated indicates cs ran but refused to do work because
// CS_ACCESS_TOKEN is unset. cs prints a PAT-setup notice and exits 0,
// so we detect it by content rather than exit code.
var ErrCSNotAuthenticated = errors.New("local: codescene `cs` CLI is not authenticated — set CS_ACCESS_TOKEN (https://codescene.io/users/me/pat)")

// WarnUnparsableCSTo is the sink used when `cs review --output-format
// json` emits non-JSON output or unrecognised schema. Overridable in
// tests; wired to stderr in main.
var WarnUnparsableCSTo = func(msg string) { _ = msg }

// HasCSCLI reports whether the configured `cs` binary is invokable.
func HasCSCLI(bin string) bool {
	if bin == "" {
		bin = "cs"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// ScoreFileCS scores one file by shelling out to
// `cs review --output-format json <path>`.
//
// `cs review` is CodeScene's per-file scoring command on current CLI
// releases; `cs check` is a lint-style command that does not support a
// JSON output flag. Real output shape is documented in the cs-review
// help text and confirmed against the binary:
//
//	{
//	  "score": 9.6,
//	  "review": [
//	    {
//	      "category":    "Complex Method",
//	      "description": "...",
//	      "indication":  2,
//	      "functions":   [{"title":"Big","details":"cc = 21","start-line":3,"end-line":25,"url":""}]
//	    }
//	  ]
//	}
//
// When the binary is missing, ErrCSNotFound is returned. When cs runs
// but is unauthenticated, ErrCSNotAuthenticated is returned so callers
// (especially in --strict mode) see the configuration gap instead of a
// silent neutral pass.
func ScoreFileCS(ctx context.Context, bin, path string) (*Score, error) {
	if bin == "" {
		bin = "cs"
	}
	if !HasCSCLI(bin) {
		return nil, ErrCSNotFound
	}
	cmd := exec.CommandContext(ctx, bin, "review", "--output-format", "json", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// `cs review` exits non-zero on findings; prefer stdout for the JSON
	// body and fall back to stderr only if stdout is empty. This keeps
	// findings intact when the CLI signals "issues present" via exit code.
	out := stdout.Bytes()
	if len(out) == 0 {
		out = stderr.Bytes()
	}
	if looksUnauthenticated(out) {
		return nil, ErrCSNotAuthenticated
	}
	if len(out) == 0 {
		if runErr != nil {
			return nil, runErr
		}
		return &Score{Path: path, Health: 10, Backend: "cs"}, nil
	}
	return parseCSCheck(out, path)
}

// looksUnauthenticated detects the "Personal Access Token required"
// notice that cs prints (and exits 0) when CS_ACCESS_TOKEN is unset.
func looksUnauthenticated(b []byte) bool {
	return bytes.Contains(b, []byte("CS_ACCESS_TOKEN")) ||
		bytes.Contains(b, []byte("Personal Access Token"))
}

// parseCSCheck parses one cs-review JSON document. When the body has no
// JSON object (e.g. cs emitted "Invalid path: ..." for files it doesn't
// recognise), it degrades to a neutral score and warns. When the body
// looks like JSON but fails to parse, the error is propagated so a
// strict caller can fail loudly instead of silently masking the issue.
func parseCSCheck(out []byte, path string) (*Score, error) {
	body := bytes.TrimSpace(out)
	objStart := bytes.IndexByte(body, '{')
	if objStart < 0 {
		WarnUnparsableCSTo(fmt.Sprintf(
			"codehealth: cs review produced no JSON for %q; degrading to neutral score. Raw: %s",
			path, truncate(out, 200)))
		return &Score{Path: path, Health: 10, Backend: "cs"}, nil
	}
	body = body[objStart:]

	var raw csReviewDoc
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("local: cs review %q: parse JSON: %w; raw=%s",
			path, err, truncate(out, 200))
	}

	// Schema-drift guard: if cs emits JSON but uses different top-level
	// keys (a renamed `review` → `issues`, say), the typed Unmarshal
	// silently zeroes everything and the caller sees a perfect-looking
	// pass. Warn so --strict users notice the parser fell out of sync.
	var probe map[string]json.RawMessage
	if json.Unmarshal(body, &probe) == nil && len(probe) > 0 {
		if _, hasScore := probe["score"]; !hasScore {
			if _, hasReview := probe["review"]; !hasReview {
				WarnUnparsableCSTo(fmt.Sprintf(
					"codehealth: cs review JSON for %q has none of {score,review}; observed keys: %v. The codehealth parser may need updating.",
					path, sortedKeys(probe)))
			}
		}
	}

	health := raw.Score
	if health == 0 {
		health = 10
	}

	smells := make([]Smell, 0)
	for _, issue := range raw.Review {
		kind := normaliseKind(issue.Category)
		message := issue.Description
		if len(issue.Functions) == 0 {
			// File-level issue (e.g. Code Duplication across the file).
			smells = append(smells, Smell{
				Kind:    kind,
				Message: strings.TrimSpace(message),
			})
			continue
		}
		for _, fn := range issue.Functions {
			msg := strings.TrimSpace(fn.Details)
			if msg == "" {
				msg = strings.TrimSpace(message)
			}
			smells = append(smells, Smell{
				Function: fn.Title,
				Line:     fn.StartLine,
				Kind:     kind,
				Message:  msg,
			})
		}
	}
	return &Score{
		Path:    path,
		Health:  health,
		Backend: "cs",
		Smells:  smells,
	}, nil
}

// csReviewDoc mirrors the documented top-level shape of
// `cs review --output-format json`.
type csReviewDoc struct {
	Score  float64         `json:"score"`
	Review []csReviewIssue `json:"review"`
}

type csReviewIssue struct {
	Category    string         `json:"category"`
	Description string         `json:"description"`
	Indication  int            `json:"indication"`
	Functions   []csReviewFunc `json:"functions"`
}

type csReviewFunc struct {
	Title     string `json:"title"`
	Details   string `json:"details"`
	StartLine int    `json:"start-line"`
	EndLine   int    `json:"end-line"`
	URL       string `json:"url"`
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normaliseKind(code string) string {
	lc := strings.ToLower(code)
	switch {
	case strings.Contains(lc, "complex"):
		return "complexity"
	case strings.Contains(lc, "nest"):
		return "deep_nesting"
	case strings.Contains(lc, "long"):
		return "long_function"
	case strings.Contains(lc, "bumpy"):
		return "bumpy_road"
	case strings.Contains(lc, "duplicat"):
		return "duplication"
	default:
		if code != "" {
			return code
		}
		return "smell"
	}
}
