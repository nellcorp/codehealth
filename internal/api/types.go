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
	Path                      string                 `json:"path"`
	Language                  string                 `json:"language"`
	LinesOfCode               int                    `json:"lines_of_code"`
	ChangeFrequency           int                    `json:"change_frequency"`
	Hotspot                   bool                   `json:"hotspot"`
	CodeHealth                rawCodeHealth          `json:"code_health"`
	CodeHealthRuleViolations  []Biomarker            `json:"code_health_rule_violations"`
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
