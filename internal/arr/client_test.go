package arr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRefreshMonitoredDownloadsPostsCommand(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAPIKey string
		gotBody   map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-Api-Key")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"name":"RefreshMonitoredDownloads"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")

	if err := c.RefreshMonitoredDownloads(context.Background()); err != nil {
		t.Fatalf("RefreshMonitoredDownloads() error = %v, want nil", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v3/command" {
		t.Errorf("path = %q, want /api/v3/command", gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("X-Api-Key = %q, want test-key", gotAPIKey)
	}
	if gotBody["name"] != "RefreshMonitoredDownloads" {
		t.Errorf("body name = %v, want RefreshMonitoredDownloads", gotBody["name"])
	}
}

func TestClientRefreshMonitoredDownloadsErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad-key")

	err := c.RefreshMonitoredDownloads(context.Background())
	if err == nil {
		t.Fatal("RefreshMonitoredDownloads() error = nil, want error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want it to mention status 401", err.Error())
	}
}

func TestClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/", "test-key")

	if err := c.RefreshMonitoredDownloads(context.Background()); err != nil {
		t.Fatalf("RefreshMonitoredDownloads() error = %v, want nil", err)
	}

	if gotPath != "/api/v3/command" {
		t.Errorf("path = %q, want /api/v3/command (no double slash)", gotPath)
	}
}
