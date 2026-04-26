package api

// ProjectHealth mirrors the subset of /v2/projects/{id} we consume.
type ProjectHealth struct {
	Hotspot float64 `json:"hotspot"`
	Average float64 `json:"average"`
}

// Hotspot represents one entry from the hotspot list.
type Hotspot struct {
	Path       string  `json:"path"`
	Health     float64 `json:"health"`
	RelativeChurn float64 `json:"relative_churn,omitempty"`
	Complexity float64 `json:"complexity,omitempty"`
}

// FileHealth describes the per-file health response.
type FileHealth struct {
	Path       string      `json:"path"`
	Health     float64     `json:"health"`
	Biomarkers []Biomarker `json:"biomarkers,omitempty"`
}

// Biomarker is a single code-smell entry.
type Biomarker struct {
	Code        string  `json:"code"`        // e.g. "deep_nested_complexity"
	Description string  `json:"description"`
	Function    string  `json:"function,omitempty"`
	StartLine   int     `json:"start_line,omitempty"`
	EndLine     int     `json:"end_line,omitempty"`
	Severity    string  `json:"severity,omitempty"`
	Score       float64 `json:"score,omitempty"`
}
