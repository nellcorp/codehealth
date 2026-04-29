package codecov

// ProjectCoverage is the project-level coverage rollup.
type ProjectCoverage struct {
	Slug         string  `json:"slug"`
	Coverage     float64 `json:"coverage"`        // percentage 0..100
	DefaultBranch string `json:"default_branch,omitempty"`
}

// FileCoverage is per-file coverage at a specific ref.
type FileCoverage struct {
	Path     string  `json:"path"`
	Ref      string  `json:"ref,omitempty"`
	Coverage float64 `json:"coverage"`
	Lines    int     `json:"lines"`
	Hits     int     `json:"hits"`
	Misses   int     `json:"misses"`
	Partials int     `json:"partials"`
}

// CoverageDelta describes coverage diff between two commits.
type CoverageDelta struct {
	Base         string  `json:"base"`
	Head         string  `json:"head"`
	BaseCoverage float64 `json:"base_coverage"`
	HeadCoverage float64 `json:"head_coverage"`
	Delta        float64 `json:"delta"` // head - base (percentage points)
	FilesChanged int     `json:"files_changed"`
}
