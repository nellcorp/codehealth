package delta

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nellcorp/codescene-mcp/internal/local"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, string(out))
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@e")
	run("config", "user.name", "t")
	return dir
}

func TestRunDetectsRegression(t *testing.T) {
	dir := gitInit(t)
	path := filepath.Join(dir, "x.go")
	clean := "package x\n\nfunc A() int { return 1 }\n"
	if err := os.WriteFile(path, []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "init")

	body := "package x\n\nfunc A(a int) int {\n  total := 0\n"
	for i := 0; i < 25; i++ {
		body += "  if a > 0 { total++ } else { total-- }\n"
	}
	body += "  return total\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")

	res, err := Run(context.Background(), Options{
		Repo:    dir,
		Staged:  true,
		Backend: &goOnly{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Regressed {
		t.Fatalf("expected regression; got %+v", res)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file; got %+v", res.Files)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, string(out))
	}
}

type goOnly struct{}

func (goOnly) Score(_ context.Context, p string) (*local.Score, error) {
	return local.ScoreGoFile(p)
}
func (goOnly) Name() string { return "go-fallback" }
