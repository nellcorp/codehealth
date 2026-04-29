// Package thresholds parses .codescene-thresholds / .codecov-thresholds
// KEY=VALUE files.
package thresholds

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Thresholds holds floor values from .codescene-thresholds and .codecov-thresholds.
//
// CodeScene keys: HOTSPOT_THRESHOLD, AVERAGE_THRESHOLD (0..10 scale).
// Codecov keys: COVERAGE_THRESHOLD (percentage 0..100),
// COVERAGE_DELTA_THRESHOLD (allowed coverage drop, percentage points, e.g. -0.5).
type Thresholds struct {
	Hotspot       float64
	Average       float64
	Coverage      float64
	CoverageDelta float64
	Raw           map[string]string
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
	if v, ok := t.Raw["COVERAGE_THRESHOLD"]; ok {
		t.Coverage, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := t.Raw["COVERAGE_DELTA_THRESHOLD"]; ok {
		t.CoverageDelta, _ = strconv.ParseFloat(v, 64)
	}
	return t, nil
}

// String renders a one-line summary suitable for CLI output.
func (t *Thresholds) String() string {
	if t == nil || t.Raw == nil {
		return "(no thresholds file found)"
	}
	return fmt.Sprintf("HOTSPOT=%.2f AVERAGE=%.2f COVERAGE=%.2f COVERAGE_DELTA=%+.2f",
		t.Hotspot, t.Average, t.Coverage, t.CoverageDelta)
}
