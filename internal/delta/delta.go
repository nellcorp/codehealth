// Package delta computes per-file health deltas between git HEAD and the
// working tree (or the index). Used by the pre-commit hook and the MCP
// `delta_check` tool.
package delta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nellcorp/codehealth/internal/local"
)

// Result describes the regression status for a batch of files.
type Result struct {
	Files     []local.Delta `json:"files"`
	NetDelta  float64       `json:"net_delta"`
	Backend   string        `json:"backend"`
	Regressed bool          `json:"regressed"`
}

// Options controls which files to score and against what baseline.
type Options struct {
	Repo      string   // repo root (defaults to cwd)
	Paths     []string // explicit files; if empty and Staged is true, uses staged Go files
	Staged    bool     // resolve paths from `git diff --cached --name-only --diff-filter=ACM`
	BaseRev   string   // git rev to diff against (default "HEAD")
	Backend   local.Backend
}

// Run scores the requested files before/after and returns aggregated deltas.
func Run(ctx context.Context, opts Options) (*Result, error) {
	repo := opts.Repo
	if repo == "" {
		var err error
		repo, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	base := opts.BaseRev
	if base == "" {
		base = "HEAD"
	}
	backend := opts.Backend
	if backend == nil {
		backend = local.Detect("")
	}

	paths := opts.Paths
	if len(paths) == 0 && opts.Staged {
		var err error
		paths, err = stagedFiles(repo)
		if err != nil {
			return nil, err
		}
	}
	if len(paths) == 0 {
		return &Result{Backend: backend.Name()}, nil
	}

	res := &Result{Backend: backend.Name(), Files: make([]local.Delta, 0, len(paths))}
	for _, p := range paths {
		// Only score files we can analyse with the chosen backend; others are skipped silently.
		if backend.Name() == "go-fallback" && !strings.HasSuffix(p, ".go") {
			continue
		}

		afterPath := filepath.Join(repo, p)
		afterScore, err := backend.Score(ctx, afterPath)
		if err != nil {
			return nil, fmt.Errorf("score after %s: %w", p, err)
		}

		beforePath, cleanup, err := writeBaseRev(ctx, repo, p, base)
		if err != nil {
			return nil, err
		}
		var before float64 = 10 // missing baseline = treat as untouched 10
		if beforePath != "" {
			beforeScore, err := backend.Score(ctx, beforePath)
			if err == nil {
				before = beforeScore.Health
			}
		}
		cleanup()

		d := local.Delta{
			Path:    p,
			Before:  before,
			After:   afterScore.Health,
			Delta:   afterScore.Health - before,
			Backend: backend.Name(),
		}
		// Smells_added: smells in after that weren't in before. Approximate
		// match by (function, kind).
		d.SmellsAdded = afterScore.Smells // baseline smells not retained; conservative
		res.Files = append(res.Files, d)
		res.NetDelta += d.Delta
	}
	res.Regressed = res.NetDelta < 0
	return res, nil
}

func stagedFiles(repo string) ([]string, error) {
	cmd := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only", "--diff-filter=ACM")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("git diff --cached: %s", string(ee.Stderr))
		}
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// writeBaseRev writes `git show base:path` to a tmp file and returns its
// path. If the file did not exist at base (new file), returns ("", noopCleanup, nil).
func writeBaseRev(ctx context.Context, repo, path, base string) (string, func(), error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "show", base+":"+path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// Treat any failure as "no baseline" — likely new file. Caller
		// uses default before=10 in that case.
		return "", func() {}, nil
	}
	tmp, err := os.CreateTemp("", "codescene-base-*"+filepath.Ext(path))
	if err != nil {
		return "", func() {}, err
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", func() {}, err
	}
	tmp.Close()
	cleanup := func() { os.Remove(tmp.Name()) }
	return tmp.Name(), cleanup, nil
}
