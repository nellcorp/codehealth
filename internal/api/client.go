// Package api wraps the CodeScene REST API.
//
// The endpoints used (subject to vendor change):
//   GET /v2/projects/{id}                   project-level scores
//   GET /v2/projects/{id}/hotspots          hotspot list
//   GET /v2/projects/{id}/files/health      per-file health + biomarkers
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
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
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
// The CI workflow uses jq paths .analysis.hotspot_code_health.now and
// .analysis.code_health.now. We replicate that contract exactly.
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

// Hotspots lists the top N hotspots in the project.
func (c *Client) Hotspots(ctx context.Context, limit int) ([]Hotspot, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var raw struct {
		Hotspots []Hotspot `json:"hotspots"`
	}
	if err := c.do(ctx, "/v2/projects/"+c.ProjectID+"/hotspots", q, &raw); err != nil {
		return nil, err
	}
	return raw.Hotspots, nil
}

// FileHealth fetches health and biomarkers for a single file.
func (c *Client) FileHealth(ctx context.Context, path string) (*FileHealth, error) {
	q := url.Values{"file": []string{path}}
	var fh FileHealth
	if err := c.do(ctx, "/v2/projects/"+c.ProjectID+"/files/health", q, &fh); err != nil {
		return nil, err
	}
	if fh.Path == "" {
		fh.Path = path
	}
	return &fh, nil
}
