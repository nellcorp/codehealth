// Package local computes per-file complexity metrics on the working tree.
//
// Two backends:
//   1. CodeScene `cs` CLI — preferred, matches CI engine.
//   2. Pure-Go fallback (gocyclo + gocognit) — Go-only, approximate.
//
// Backend selection: detect.go picks based on PATH lookup of `cs`.
package local

// Score is a per-file health score. Higher == healthier (10.0 max,
// matching CodeScene). Approximate when produced by the pure-Go fallback.
type Score struct {
	Path    string
	Health  float64
	Backend string // "cs" or "go-fallback"
	Smells  []Smell
}

// Smell is a single complexity finding (function-level).
type Smell struct {
	Function string
	Line     int
	Kind     string // "cyclomatic", "cognitive", "long_function"
	Value    int
	Message  string
}

// Delta describes the before/after comparison for a changed file.
type Delta struct {
	Path        string  `json:"path"`
	Before      float64 `json:"before"`
	After       float64 `json:"after"`
	Delta       float64 `json:"delta"` // After - Before
	Backend     string  `json:"backend"`
	SmellsAdded []Smell `json:"smells_added,omitempty"`
}
