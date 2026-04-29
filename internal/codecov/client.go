// Package codecov wraps the Codecov REST API (v2).
//
// Endpoints used:
//
//	GET /api/v2/{service}/{owner}/repos/{repo}/                 → project coverage
//	GET /api/v2/{service}/{owner}/repos/{repo}/report/?branch=  → per-file coverage
//	GET /api/v2/{service}/{owner}/repos/{repo}/compare/?base=&head= → coverage delta
//
// Auth: Bearer token from CODECOV_TOKEN.
package codecov

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin wrapper over net/http.
type Client struct {
	BaseURL    string
	Token      string
	Service    string // github, gitlab, bitbucket, ...
	Owner      string
	Repo       string
	HTTPClient *http.Client
}

// New returns a Client. slug must be "service/owner/repo" (e.g. "github/nellcorp/codehealth").
// Returns a Client with empty service/owner/repo if slug is malformed; CoverageReady-style
// guards in callers should prevent that path from being reached.
func New(baseURL, token, slug string) *Client {
	c := &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
	parts := strings.SplitN(slug, "/", 3)
	if len(parts) == 3 {
		c.Service, c.Owner, c.Repo = parts[0], parts[1], parts[2]
	}
	return c
}

func (c *Client) repoPath() string {
	return fmt.Sprintf("/api/v2/%s/%s/repos/%s", c.Service, c.Owner, c.Repo)
}

func (c *Client) do(ctx context.Context, path string, query url.Values, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
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
		return fmt.Errorf("codecov: %s %s: %s", resp.Status, u, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ProjectCoverage fetches the repo-level coverage rollup.
//
// Codecov's repo endpoint returns a `totals.coverage` field (percentage).
func (c *Client) ProjectCoverage(ctx context.Context) (*ProjectCoverage, error) {
	var raw struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"branch"`
		Totals        struct {
			Coverage float64 `json:"coverage"`
		} `json:"totals"`
	}
	if err := c.do(ctx, c.repoPath()+"/", nil, &raw); err != nil {
		return nil, err
	}
	return &ProjectCoverage{
		Slug:          fmt.Sprintf("%s/%s/%s", c.Service, c.Owner, c.Repo),
		Coverage:      raw.Totals.Coverage,
		DefaultBranch: raw.DefaultBranch,
	}, nil
}

// FileCoverage fetches coverage for one path at the given ref. ref may be a
// branch name or commit SHA; empty ref defaults to the repo's default branch.
func (c *Client) FileCoverage(ctx context.Context, ref, path string) (*FileCoverage, error) {
	q := url.Values{}
	if ref != "" {
		// Heuristic: 40-char hex looks like a SHA.
		if looksLikeSHA(ref) {
			q.Set("sha", ref)
		} else {
			q.Set("branch", ref)
		}
	}
	q.Set("path", path)

	var raw struct {
		Files []struct {
			Name   string `json:"name"`
			Totals struct {
				Coverage float64 `json:"coverage"`
				Lines    int     `json:"lines"`
				Hits     int     `json:"hits"`
				Misses   int     `json:"misses"`
				Partials int     `json:"partials"`
			} `json:"totals"`
		} `json:"files"`
	}
	if err := c.do(ctx, c.repoPath()+"/report/", q, &raw); err != nil {
		return nil, err
	}
	for i := range raw.Files {
		if raw.Files[i].Name == path {
			f := raw.Files[i]
			return &FileCoverage{
				Path:     f.Name,
				Ref:      ref,
				Coverage: f.Totals.Coverage,
				Lines:    f.Totals.Lines,
				Hits:     f.Totals.Hits,
				Misses:   f.Totals.Misses,
				Partials: f.Totals.Partials,
			}, nil
		}
	}
	if len(raw.Files) > 0 {
		f := raw.Files[0]
		return &FileCoverage{
			Path:     f.Name,
			Ref:      ref,
			Coverage: f.Totals.Coverage,
			Lines:    f.Totals.Lines,
			Hits:     f.Totals.Hits,
			Misses:   f.Totals.Misses,
			Partials: f.Totals.Partials,
		}, nil
	}
	return nil, fmt.Errorf("codecov: no file matching %q", path)
}

// Compare returns coverage delta between base and head commits.
func (c *Client) Compare(ctx context.Context, base, head string) (*CoverageDelta, error) {
	q := url.Values{"base": []string{base}, "head": []string{head}}

	var raw struct {
		BaseCommit struct {
			Totals struct {
				Coverage float64 `json:"coverage"`
			} `json:"totals"`
		} `json:"base_commit"`
		HeadCommit struct {
			Totals struct {
				Coverage float64 `json:"coverage"`
			} `json:"totals"`
		} `json:"head_commit"`
		Files []json.RawMessage `json:"files"`
	}
	if err := c.do(ctx, c.repoPath()+"/compare/", q, &raw); err != nil {
		return nil, err
	}
	return &CoverageDelta{
		Base:         base,
		Head:         head,
		BaseCoverage: raw.BaseCommit.Totals.Coverage,
		HeadCoverage: raw.HeadCommit.Totals.Coverage,
		Delta:        raw.HeadCommit.Totals.Coverage - raw.BaseCommit.Totals.Coverage,
		FilesChanged: len(raw.Files),
	}, nil
}

func looksLikeSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
