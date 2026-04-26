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

func TestHotspots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "5" {
			t.Errorf("limit query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hotspots": []map[string]any{
				{"path": "a.go", "health": 6.0},
				{"path": "b.go", "health": 7.5},
			},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok", "42").Hotspots(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "a.go" {
		t.Fatalf("got %+v", got)
	}
}
