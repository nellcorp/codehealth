// Package thresholds parses .codescene-thresholds-style KEY=VALUE files.
package thresholds

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Thresholds holds the floor values from .codescene-thresholds.
type Thresholds struct {
	Hotspot float64
	Average float64
	Raw     map[string]string
}

// Load parses a KEY=VALUE file. Lines starting with # are comments. Missing
// file returns an empty Thresholds with Raw == nil and no error so callers
// can degrade gracefully.
func Load(path string) (*Thresholds, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Thresholds{}, nil
		}
		return nil, err
	}
	defer f.Close()

	t := &Thresholds{Raw: map[string]string{}}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.Trim(val, `"'`))
		t.Raw[key] = val
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	if v, ok := t.Raw["HOTSPOT_THRESHOLD"]; ok {
		t.Hotspot, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := t.Raw["AVERAGE_THRESHOLD"]; ok {
		t.Average, _ = strconv.ParseFloat(v, 64)
	}
	return t, nil
}

// String renders a one-line summary suitable for CLI output.
func (t *Thresholds) String() string {
	if t == nil || t.Raw == nil {
		return "(no .codescene-thresholds found)"
	}
	return fmt.Sprintf("HOTSPOT_THRESHOLD=%.2f AVERAGE_THRESHOLD=%.2f", t.Hotspot, t.Average)
}
