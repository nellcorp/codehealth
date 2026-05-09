package api

import (
	"encoding/json"
	"fmt"
)

// ProjectHealth mirrors the subset of /v2/projects/{id} we consume.
type ProjectHealth struct {
	Hotspot float64 `json:"hotspot"`
	Average float64 `json:"average"`
}

// Hotspot represents one file from the analyses-files endpoint with the
// hotspot flag set.
type Hotspot struct {
	Path            string  `json:"path"`
	Health          float64 `json:"health"`
	ChangeFrequency int     `json:"change_frequency"`
	LinesOfCode     int     `json:"lines_of_code"`
	Language        string  `json:"language,omitempty"`
}

// FileHealth describes the per-file response.
type FileHealth struct {
	Path            string      `json:"path"`
	Health          float64     `json:"health"`
	Language        string      `json:"language,omitempty"`
	ChangeFrequency int         `json:"change_frequency"`
	LinesOfCode     int         `json:"lines_of_code"`
	Biomarkers      []Biomarker `json:"biomarkers,omitempty"`
}

// Biomarker is a single code-smell entry. Mapped from
// `code_health_rule_violations[]`.
type Biomarker struct {
	CodeSmell string `json:"code_smell"`
	RuleSet   string `json:"rule_set"`
	Count     int    `json:"count,omitempty"`
}

// rawFile is the on-the-wire shape returned by /analyses/.../files.
//
// CodeScene encodes scores as either a JSON number or the literal string
// "-" when no score is available. flexFloat handles both.
type rawFile struct {
	Path                     string        `json:"path"`
	Language                 string        `json:"language"`
	LinesOfCode              int           `json:"lines_of_code"`
	ChangeFrequency          int           `json:"change_frequency"`
	Hotspot                  bool          `json:"hotspot"`
	CodeHealth               rawCodeHealth `json:"code_health"`
	CodeHealthRuleViolations []Biomarker   `json:"code_health_rule_violations"`
}

type rawCodeHealth struct {
	CurrentScore flexFloat `json:"current_score"`
}

// flexFloat decodes either a JSON number or the literal "-" / null.
type flexFloat struct {
	Value float64
	Set   bool
}

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "-" || s == "" {
			return nil
		}
		var v float64
		if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
			return nil
		}
		f.Value = v
		f.Set = true
		return nil
	}
	if err := json.Unmarshal(b, &f.Value); err != nil {
		return err
	}
	f.Set = true
	return nil
}

// CodeReviewList is the paginated wrapper returned by
// GET /v2/projects/{id}/delta-analyses.
type CodeReviewList struct {
	Page     int          `json:"page"`
	MaxPages int          `json:"max_pages"`
	Reviews  []CodeReview `json:"reviews"`
}

// CodeReview is the shape returned by both /delta-analyses (list items)
// and /delta-analyses/{id} (full detail). Detail responses include
// FileResults; list items typically omit them.
type CodeReview struct {
	ID              json.Number       `json:"id,omitempty"`
	ProjectID       json.Number       `json:"project_id,omitempty"`
	Repository      string            `json:"repository,omitempty"`
	BaseRef         string            `json:"base_ref,omitempty"`
	BranchHead      string            `json:"delta_branch_head,omitempty"`
	Commits         []string          `json:"commits,omitempty"`
	Authors         []string          `json:"authors,omitempty"`
	AnalysisTime    json.RawMessage   `json:"analysistime,omitempty"`
	CodeHealth      *float64          `json:"code_health,omitempty"`
	OldCodeHealth   *float64          `json:"old_code_health,omitempty"`
	EnabledGates    json.RawMessage   `json:"enabled_gates,omitempty"`
	FailedGates     json.RawMessage   `json:"failed_gates,omitempty"`
	ExternalReview  string            `json:"external_review_id,omitempty"`
	FileResults     []CodeReviewFile  `json:"file_results,omitempty"`
	Directives      json.RawMessage   `json:"directives,omitempty"`
}

// CodeReviewFile is one entry in CodeReview.FileResults — per-file
// before/after code health.
type CodeReviewFile struct {
	File          string   `json:"file"`
	LOC           int      `json:"loc,omitempty"`
	OldLOC        int      `json:"old-loc,omitempty"`
	CodeHealth    *float64 `json:"code_health,omitempty"`
	OldCodeHealth *float64 `json:"old_code_health,omitempty"`
}

// Component is one entry from /v2/projects/{id}/analyses/latest/components.
type Component struct {
	Name            string             `json:"name"`
	Ref             string             `json:"ref,omitempty"`
	Age             int                `json:"age,omitempty"`
	ChangeFrequency int                `json:"change_frequency,omitempty"`
	LinesOfCode     int                `json:"lines_of_code,omitempty"`
	SystemHealth    componentHealthRef `json:"system_health,omitempty"`
}

type componentHealthRef struct {
	CurrentScore flexFloat `json:"current_score"`
}

// MarshalJSON flattens system_health.current_score to a plain number for
// downstream readability.
func (c Component) MarshalJSON() ([]byte, error) {
	type alias struct {
		Name            string  `json:"name"`
		Ref             string  `json:"ref,omitempty"`
		Age             int     `json:"age,omitempty"`
		ChangeFrequency int     `json:"change_frequency,omitempty"`
		LinesOfCode     int     `json:"lines_of_code,omitempty"`
		Health          float64 `json:"health,omitempty"`
	}
	return json.Marshal(alias{
		Name:            c.Name,
		Ref:             c.Ref,
		Age:             c.Age,
		ChangeFrequency: c.ChangeFrequency,
		LinesOfCode:     c.LinesOfCode,
		Health:          c.SystemHealth.CurrentScore.Value,
	})
}
