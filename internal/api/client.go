// Package api wraps the CodeScene REST API (v2).
//
// Endpoints used:
//
//	GET /v2/projects/{id}                          → project-level scores
//	GET /v2/projects/{id}/analyses/latest/files    → file-level health
//	                                                 (filter=hotspot:true for hotspots,
//	                                                  filter=path:<p> for one file)
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

func (c *Client) do(ctx context.Context, path string, query url.Values, out any) error {
	u := fmt.Sprintf("%s%s", c.BaseURL, path)
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

func maxPageSize(limit int) int {
	if limit < 50 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}
