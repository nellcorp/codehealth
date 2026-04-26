package local

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGo(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScoreGoFileSimple(t *testing.T) {
	path := writeGo(t, `package x

func A() int { return 1 }
`)
	got, err := ScoreGoFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Health < 9 {
		t.Fatalf("trivial file should score high; got %v", got.Health)
	}
	if len(got.Smells) != 0 {
		t.Fatalf("expected no smells, got %+v", got.Smells)
	}
}

func TestScoreGoFileComplex(t *testing.T) {
	body := "package x\n\nfunc Big(a int) int {\n  total := 0\n"
	for i := 0; i < 25; i++ {
		body += "  if a > 0 { total++ } else { total-- }\n"
	}
	body += "  return total\n}\n"
	path := writeGo(t, body)
	got, err := ScoreGoFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Health > 7 {
		t.Fatalf("complex file should score low; got %v", got.Health)
	}
	if len(got.Smells) == 0 {
		t.Fatal("expected at least one smell on complex function")
	}
}

func TestScoreFromWorst(t *testing.T) {
	cases := []struct {
		worst int
		min   float64
		max   float64
	}{
		{0, 10, 10},
		{5, 10, 10},
		{10, 7.9, 8.1},
		{20, 4.9, 5.1},
		{30, 1.9, 2.1},
		{50, 1, 1.01},
	}
	for _, c := range cases {
		got := scoreFromWorst(c.worst)
		if got < c.min || got > c.max {
			t.Errorf("worst=%d: got %v, want [%v,%v]", c.worst, got, c.min, c.max)
		}
	}
}
