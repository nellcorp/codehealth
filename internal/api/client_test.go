package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"analysis": map[string]any{
				"hotspot_code_health": map[string]any{"now": 9.47},
				"code_health":         map[string]any{"now": 9.18},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "42")
	got, err := c.ProjectHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Hotspot != 9.47 || got.Average != 9.18 {
		t.Fatalf("got %+v", got)
	}
}

func TestProjectHealthErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok", "42").ProjectHealth(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestHotspotsSortsAscending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter"); got != "hotspot:true" {
			t.Errorf("filter param: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page": 1, "max_pages": 1,
			"files": []map[string]any{
				{"path": "a.go", "hotspot": true, "code_health": map[string]any{"current_score": 9.5}},
				{"path": "b.go", "hotspot": true, "code_health": map[string]any{"current_score": 7.2}},
				{"path": "c.go", "hotspot": false, "code_health": map[string]any{"current_score": 3.0}},
			},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").Hotspots(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != "b.go" {
		t.Fatalf("expected b.go (lowest health) first, got %+v", got)
	}
	if got[0].Health != 7.2 {
		t.Fatalf("expected 7.2, got %v", got[0].Health)
	}
}

func TestFileHealthMatchesPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page": 1, "max_pages": 1,
			"files": []map[string]any{
				{"path": "a.go", "code_health": map[string]any{"current_score": 9}, "code_health_rule_violations": []any{}},
				{"path": "b.go", "code_health": map[string]any{"current_score": 7},
					"code_health_rule_violations": []map[string]any{
						{"code_smell": "Bumpy Road", "rule_set": "advisory", "count": 2},
					},
				},
			},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").FileHealth(context.Background(), "b.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "b.go" || got.Health != 7 {
		t.Fatalf("got %+v", got)
	}
	if len(got.Biomarkers) != 1 || got.Biomarkers[0].CodeSmell != "Bumpy Road" {
		t.Fatalf("got %+v", got.Biomarkers)
	}
}

func TestListCodeReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v2/projects/42/delta-analyses") {
			t.Errorf("path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Errorf("page: %q", got)
		}
		_, _ = w.Write([]byte(`{
			"page": 2, "max_pages": 5,
			"delta_analyses": [
				{"id": 101, "repository": "honey-cheetah", "commits": ["abc"], "code_health": 9.1, "old_code_health": 9.4}
			]
		}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").ListCodeReviews(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Page != 2 || got.MaxPages != 5 || len(got.Reviews) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got.Reviews[0].Repository != "honey-cheetah" {
		t.Fatalf("review: %+v", got.Reviews[0])
	}
	if got.Reviews[0].CodeHealth == nil || *got.Reviews[0].CodeHealth != 9.1 {
		t.Fatalf("code_health: %+v", got.Reviews[0].CodeHealth)
	}
}

func TestCodeReviewDecodesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v2/projects/42/delta-analyses/101") {
			t.Errorf("path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"id": 101,
			"project_id": 42,
			"repository": "honey-cheetah",
			"commits": ["abc","def"],
			"code_health": 9.1,
			"old_code_health": 9.4,
			"enabled_gates": {"degrades_in_code_health": true},
			"failed_gates": {"degrades_in_code_health": true},
			"file_results": [
				{"file":"a.go","loc":120,"old-loc":110,"code_health":7.2,"old_code_health":8.1}
			],
			"analysistime": "2026-05-09T10:00:00Z"
		}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").CodeReview(context.Background(), "101")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "honey-cheetah" || len(got.FileResults) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got.FileResults[0].File != "a.go" || got.FileResults[0].LOC != 120 || got.FileResults[0].OldLOC != 110 {
		t.Fatalf("file result: %+v", got.FileResults[0])
	}
	if got.FileResults[0].CodeHealth == nil || *got.FileResults[0].CodeHealth != 7.2 {
		t.Fatalf("file code_health: %+v", got.FileResults[0].CodeHealth)
	}
	if len(got.FailedGates) == 0 {
		t.Fatalf("failed_gates raw missing")
	}
}

func TestCodeReviewRequiresID(t *testing.T) {
	c := New("http://unused", "tok", "42")
	if _, err := c.CodeReview(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestKPITrendBuildsPathAndQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v2/projects/42/kpi-trend/code-health/hotspots") {
			t.Errorf("path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("start"); got != "2026-01-01" {
			t.Errorf("start: %q", got)
		}
		if got := r.URL.Query().Get("end"); got != "2026-05-09" {
			t.Errorf("end: %q", got)
		}
		_, _ = w.Write([]byte(`[{"date":"2026-01-01","kpi":9.1}]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").KPITrend(context.Background(), "code-health", "hotspots", "2026-01-01", "2026-05-09")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"kpi":9.1`) {
		t.Fatalf("payload: %s", got)
	}
}

func TestKPITrendRequiresFactor(t *testing.T) {
	c := New("http://unused", "tok", "42")
	if _, err := c.KPITrend(context.Background(), "", "", "", ""); err == nil {
		t.Fatal("expected error for empty factor")
	}
}

func TestIsValidKPIFactor(t *testing.T) {
	for _, ok := range []string{"code-health", "delivery", "knowledge", "team-code-alignment", "Code-Health"} {
		if !IsValidKPIFactor(ok) {
			t.Errorf("expected %q to be valid", ok)
		}
	}
	for _, bad := range []string{"", "code_health", "random"} {
		if IsValidKPIFactor(bad) {
			t.Errorf("expected %q to be invalid", bad)
		}
	}
}

func TestComponentsAcceptsBothShapes(t *testing.T) {
	wrapped := []byte(`{"components":[{"name":"core","system_health":{"current_score":8.2},"change_frequency":42}]}`)
	got, err := decodeComponents(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "core" || got[0].SystemHealth.CurrentScore.Value != 8.2 {
		t.Fatalf("wrapped: %+v", got)
	}

	bare := []byte(`[{"name":"api","system_health":{"current_score":"-"}}]`)
	got2, err := decodeComponents(bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 || got2[0].Name != "api" {
		t.Fatalf("bare: %+v", got2)
	}
	if got2[0].SystemHealth.CurrentScore.Set {
		t.Fatalf("\"-\" should yield unset; got %+v", got2[0].SystemHealth.CurrentScore)
	}
}

func TestComponentMarshalFlattensHealth(t *testing.T) {
	c := Component{Name: "core", ChangeFrequency: 5}
	c.SystemHealth.CurrentScore.Value = 8.2
	c.SystemHealth.CurrentScore.Set = true
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"health":8.2`) {
		t.Fatalf("expected flattened health: %s", out)
	}
	if strings.Contains(string(out), "system_health") {
		t.Fatalf("nested system_health should be hidden: %s", out)
	}
}

func TestFlexFloatHandlesDash(t *testing.T) {
	var f flexFloat
	if err := json.Unmarshal([]byte(`"-"`), &f); err != nil {
		t.Fatal(err)
	}
	if f.Set || f.Value != 0 {
		t.Fatalf("dash should yield zero/unset; got %+v", f)
	}
	if err := json.Unmarshal([]byte(`9.18`), &f); err != nil {
		t.Fatal(err)
	}
	if !f.Set || f.Value != 9.18 {
		t.Fatalf("number should set; got %+v", f)
	}
}
