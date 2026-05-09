// Package api wraps the CodeScene REST API (v2).
//
// Endpoints used:
//
//	GET /v2/projects/{id}                                          → project-level scores
//	GET /v2/projects/{id}/analyses/latest/files                    → file-level health
//	                                                                 (filter=hotspot:true for hotspots,
//	                                                                  filter=path:<p> for one file)
//	GET /v2/projects/{id}/analyses/latest/components               → architectural component scores
//	GET /v2/projects/{id}/delta-analyses                           → list past Code Reviews (PR-triggered)
//	GET /v2/projects/{id}/delta-analyses/{id}                      → one Code Review (file_results, gates, deltas)
//	GET /v2/projects/{id}/kpi-trend/{factor}[/{kpi}]               → 4-factors dashboard trend lines
//
// Auth: Bearer token from CODESCENE_TOKEN.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client is a thin wrapper over net/http.
type Client struct {
	BaseURL    string
	Token      string
	ProjectID  string
	HTTPClient *http.Client
}

// New returns a Client with sensible defaults.
func New(baseURL, token, projectID string) *Client {
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		ProjectID:  projectID,
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// do issues an authenticated GET and decodes the JSON response into out.
func (c *Client) do(ctx context.Context, path string, query url.Values, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u = u + "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("codescene: %s %s: %s", resp.Status, u, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ProjectHealth fetches /v2/projects/{id} and extracts the live scores.
//
// jq paths .analysis.hotspot_code_health.now and .analysis.code_health.now —
// matches the contract used by the CI workflow.
func (c *Client) ProjectHealth(ctx context.Context) (*ProjectHealth, error) {
	var raw struct {
		Analysis struct {
			HotspotCodeHealth struct {
				Now float64 `json:"now"`
			} `json:"hotspot_code_health"`
			CodeHealth struct {
				Now float64 `json:"now"`
			} `json:"code_health"`
		} `json:"analysis"`
	}
	if err := c.do(ctx, "/v2/projects/"+c.ProjectID, nil, &raw); err != nil {
		return nil, err
	}
	return &ProjectHealth{
		Hotspot: raw.Analysis.HotspotCodeHealth.Now,
		Average: raw.Analysis.CodeHealth.Now,
	}, nil
}

// listFiles fetches one page from /analyses/latest/files with optional filter.
func (c *Client) listFiles(ctx context.Context, q url.Values) ([]rawFile, int, error) {
	var raw struct {
		Page     int       `json:"page"`
		MaxPages int       `json:"max_pages"`
		Files    []rawFile `json:"files"`
	}
	path := "/v2/projects/" + c.ProjectID + "/analyses/latest/files"
	if err := c.do(ctx, path, q, &raw); err != nil {
		return nil, 0, err
	}
	return raw.Files, raw.MaxPages, nil
}

// Hotspots returns the top-N hotspots ordered by code_health ascending
// (worst-first). CodeScene's API does not let us sort code_health
// ascending server-side; we fetch hotspot-only files and sort client-side.
func (c *Client) Hotspots(ctx context.Context, limit int) ([]Hotspot, error) {
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{
		"page_size": []string{strconv.Itoa(maxPageSize(limit))},
		"order_by":  []string{"code_health"},
		"filter":    []string{"hotspot:true"},
	}
	files, _, err := c.listFiles(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make([]Hotspot, 0, len(files))
	for _, f := range files {
		if !f.Hotspot || !f.CodeHealth.CurrentScore.Set {
			continue
		}
		out = append(out, Hotspot{
			Path:            f.Path,
			Health:          f.CodeHealth.CurrentScore.Value,
			ChangeFrequency: f.ChangeFrequency,
			LinesOfCode:     f.LinesOfCode,
			Language:        f.Language,
		})
	}
	// Worst-first.
	sort.Slice(out, func(i, j int) bool { return out[i].Health < out[j].Health })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// FileHealth fetches health and biomarkers for a single file by path.
//
// CodeScene's "files" endpoint takes a `filter=path:<p>` parameter. The
// response is a list; we return the exact match or, failing that, the
// first match.
func (c *Client) FileHealth(ctx context.Context, path string) (*FileHealth, error) {
	q := url.Values{
		"page_size": []string{"50"},
		"filter":    []string{"path:" + path},
	}
	files, _, err := c.listFiles(ctx, q)
	if err != nil {
		return nil, err
	}
	var hit *rawFile
	for i := range files {
		if files[i].Path == path {
			hit = &files[i]
			break
		}
	}
	if hit == nil && len(files) > 0 {
		hit = &files[0]
	}
	if hit == nil {
		return nil, fmt.Errorf("codescene: no file matching %q", path)
	}
	return &FileHealth{
		Path:            hit.Path,
		Health:          hit.CodeHealth.CurrentScore.Value,
		Language:        hit.Language,
		ChangeFrequency: hit.ChangeFrequency,
		LinesOfCode:     hit.LinesOfCode,
		Biomarkers:      hit.CodeHealthRuleViolations,
	}, nil
}

// ListCodeReviews returns a paginated list of past Code Reviews
// (delta-analyses triggered by CodeScene's PR/CI integration).
func (c *Client) ListCodeReviews(ctx context.Context, page int, filter string) (*CodeReviewList, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if filter != "" {
		q.Set("filter", filter)
	}
	var raw struct {
		Page          int          `json:"page"`
		MaxPages      int          `json:"max_pages"`
		DeltaAnalyses []CodeReview `json:"delta_analyses"`
	}
	path := "/v2/projects/" + c.ProjectID + "/delta-analyses"
	if err := c.do(ctx, path, q, &raw); err != nil {
		return nil, err
	}
	return &CodeReviewList{
		Page:     raw.Page,
		MaxPages: raw.MaxPages,
		Reviews:  raw.DeltaAnalyses,
	}, nil
}

// CodeReview fetches one delta-analysis by id, including per-file
// code_health old/new and quality-gate results.
func (c *Client) CodeReview(ctx context.Context, id string) (*CodeReview, error) {
	if id == "" {
		return nil, fmt.Errorf("codescene: review id required")
	}
	var out CodeReview
	path := "/v2/projects/" + c.ProjectID + "/delta-analyses/" + url.PathEscape(id)
	if err := c.do(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Components returns architectural-component health from the latest analysis.
//
// Response shape varies by deployment — some wrap in {"components": [...]},
// some return a bare array. decodeComponents handles both.
func (c *Client) Components(ctx context.Context) ([]Component, error) {
	var raw json.RawMessage
	path := "/v2/projects/" + c.ProjectID + "/analyses/latest/components"
	if err := c.do(ctx, path, nil, &raw); err != nil {
		return nil, err
	}
	return decodeComponents(raw)
}

func decodeComponents(raw json.RawMessage) ([]Component, error) {
	var wrapped struct {
		Components []Component `json:"components"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Components != nil {
		return wrapped.Components, nil
	}
	var arr []Component
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("codescene: decode components: %w", err)
	}
	return arr, nil
}

// KPITrend fetches a CodeScene 4-factors trend line. Factor is one of
// `code-health`, `delivery`, `knowledge`, `team-code-alignment`. KPI is
// optional (factor-only paths return the headline KPI). Start and end are
// ISO-8601 dates ("YYYY-MM-DD"); empty defaults to the server's range.
//
// Response is passed through as raw JSON because the shape differs per
// factor and is documented loosely.
func (c *Client) KPITrend(ctx context.Context, factor, kpi, start, end string) (json.RawMessage, error) {
	if factor == "" {
		return nil, fmt.Errorf("codescene: kpi factor required")
	}
	path := "/v2/projects/" + c.ProjectID + "/kpi-trend/" + url.PathEscape(factor)
	if kpi != "" {
		path += "/" + url.PathEscape(kpi)
	}
	q := url.Values{}
	if start != "" {
		q.Set("start", start)
	}
	if end != "" {
		q.Set("end", end)
	}
	var raw json.RawMessage
	if err := c.do(ctx, path, q, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ValidKPIFactors lists the documented factor segments accepted by
// /v2/projects/{id}/kpi-trend/{factor}.
var ValidKPIFactors = []string{
	"code-health",
	"delivery",
	"knowledge",
	"team-code-alignment",
}

// IsValidKPIFactor reports whether s is one of the documented factors.
func IsValidKPIFactor(s string) bool {
	for _, f := range ValidKPIFactors {
		if strings.EqualFold(s, f) {
			return true
		}
	}
	return false
}

func maxPageSize(limit int) int {
	if limit < 50 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}
