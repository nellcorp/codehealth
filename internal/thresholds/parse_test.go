package thresholds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codescene-thresholds")
	body := `# CodeScene thresholds
HOTSPOT_THRESHOLD=9.47
AVERAGE_THRESHOLD=9.18
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hotspot != 9.47 {
		t.Fatalf("hotspot: want 9.47 got %v", got.Hotspot)
	}
	if got.Average != 9.18 {
		t.Fatalf("average: want 9.18 got %v", got.Average)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Raw != nil {
		t.Fatalf("missing file should yield empty Thresholds, got %+v", got)
	}
}

func TestLoadIgnoresMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codescene-thresholds")
	body := "# comment\nGARBAGE\nHOTSPOT_THRESHOLD=1.5\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hotspot != 1.5 {
		t.Fatalf("got %v", got.Hotspot)
	}
}
