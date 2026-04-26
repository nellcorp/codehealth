package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
