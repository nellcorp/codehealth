package local

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Backend is the interface used by delta and MCP tool handlers.
type Backend interface {
	Score(ctx context.Context, path string) (*Score, error)
	Name() string
}

// Detect picks the best available backend for the host.
//
// `cs` CLI is preferred when present; otherwise the pure-Go fallback is
// used. A one-time warning describes the consequence of falling back.
func Detect(csBin string) Backend {
	if HasCSCLI(csBin) {
		return &csBackend{bin: csBin}
	}
	warnFallbackOnce(csBin)
	return &goBackend{}
}

var fallbackOnce sync.Once

// WarnFallbackTo is the function used to emit the fallback warning. It is
// overridable in tests.
var WarnFallbackTo = func(msg string) { _ = msg }

func warnFallbackOnce(bin string) {
	fallbackOnce.Do(func() {
		if bin == "" {
			bin = "cs"
		}
		WarnFallbackTo(fmt.Sprintf(
			"codehealth-mcp: %q not found on PATH; using gocyclo+gocognit fallback. "+
				"Install CodeScene CLI for engine-accurate local scoring: "+
				"https://codescene.io/docs/guides/cli/index.html", bin))
	})
}

// csBackend invokes the cs CLI.
type csBackend struct{ bin string }

func (b *csBackend) Score(ctx context.Context, path string) (*Score, error) {
	return ScoreFileCS(ctx, b.bin, path)
}
func (b *csBackend) Name() string { return "cs" }

// goBackend uses the pure-Go heuristic. Non-Go files return a neutral score.
type goBackend struct{}

func (b *goBackend) Score(_ context.Context, path string) (*Score, error) {
	if !strings.HasSuffix(path, ".go") {
		return &Score{Path: path, Health: 10, Backend: "go-fallback"}, nil
	}
	return ScoreGoFile(path)
}
func (b *goBackend) Name() string { return "go-fallback" }
