package codecov

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectCoverage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token tok" {
			t.Errorf("auth header: got %q", got)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v2/github/nellcorp/repos/codehealth/") {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":   "codehealth",
			"branch": "main",
			"totals": map[string]any{"coverage": 87.42},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "github/nellcorp/codehealth")
	got, err := c.ProjectCoverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Coverage != 87.42 || got.DefaultBranch != "main" {
		t.Fatalf("got %+v", got)
	}
	if got.Slug != "github/nellcorp/codehealth" {
		t.Fatalf("slug: got %q", got.Slug)
	}
}

func TestProjectCoverageErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"nope"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok", "github/o/r").ProjectCoverage(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestFileCoverageMatchesPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("branch"); got != "main" {
			t.Errorf("branch param: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "a.go", "totals": map[string]any{"coverage": 50.0, "lines": 10, "hits": 5, "misses": 5, "partials": 0}},
				{"name": "b.go", "totals": map[string]any{"coverage": 90.0, "lines": 20, "hits": 18, "misses": 2, "partials": 0}},
			},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "github/o/r").FileCoverage(context.Background(), "main", "b.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "b.go" || got.Coverage != 90.0 || got.Lines != 20 {
		t.Fatalf("got %+v", got)
	}
}

func TestFileCoverageSHAUsesShaQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sha"); got != "abc1234" {
			t.Errorf("sha param: %q", got)
		}
		if got := r.URL.Query().Get("branch"); got != "" {
			t.Errorf("branch should be empty for sha; got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "a.go", "totals": map[string]any{"coverage": 100.0}},
			},
		})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok", "github/o/r").FileCoverage(context.Background(), "abc1234", "a.go")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("base") != "deadbeef" || q.Get("head") != "feedface" {
			t.Errorf("query: %v", q)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_commit": map[string]any{"totals": map[string]any{"coverage": 80.0}},
			"head_commit": map[string]any{"totals": map[string]any{"coverage": 78.5}},
			"files":       []map[string]any{{"name": "x.go"}, {"name": "y.go"}},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "github/o/r").Compare(context.Background(), "deadbeef", "feedface")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseCoverage != 80.0 || got.HeadCoverage != 78.5 {
		t.Fatalf("got %+v", got)
	}
	if got.Delta != -1.5 {
		t.Fatalf("delta: got %v", got.Delta)
	}
	if got.FilesChanged != 2 {
		t.Fatalf("files: got %v", got.FilesChanged)
	}
}

func TestLooksLikeSHA(t *testing.T) {
	cases := map[string]bool{
		"abc1234":                                  true,
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef": true,
		"main":                                     false,
		"feature/x":                                false,
		"abc":                                      false, // too short
		"abcg":                                     false, // non-hex
	}
	for in, want := range cases {
		if got := looksLikeSHA(in); got != want {
			t.Errorf("looksLikeSHA(%q) = %v, want %v", in, got, want)
		}
	}
}
