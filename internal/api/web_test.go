package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/mocks"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveDashboard resolves the shell relative to the working directory, so the
// test plants one there rather than reaching into the repo's real web/static.
func withDashboardShell(t *testing.T, body string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "web", "static"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "web", "static", "v2.html"), []byte(body), 0o644))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func newWebRouter(t *testing.T) *mux.Router {
	t.Helper()

	handlers := NewHandlers(mocks.NewMockJobQueue(t), mocks.NewMockGatekeeper(t), &config.Config{}, nil, nil)
	r := mux.NewRouter()
	handlers.registerWebRoutes(r)
	return r
}

// Every client-side route has to serve the same shell, otherwise a deep link
// or a refresh on /jobs 404s instead of loading the app.
func TestSPARoutesServeDashboard(t *testing.T) {
	withDashboardShell(t, "<html>grabarr shell</html>")
	r := newWebRouter(t)

	for _, path := range []string{"/", "/jobs", "/settings"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), "grabarr shell")
		})
	}
}

func TestUnknownRouteNotFound(t *testing.T) {
	withDashboardShell(t, "<html>grabarr shell</html>")
	r := newWebRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServeDashboardMissingShell(t *testing.T) {
	withDashboardShell(t, "")
	require.NoError(t, os.Remove(filepath.Join("web", "static", "v2.html")))
	r := newWebRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A trailing slash should reach the app, not a 404. The client tolerates it
// too, but the canonical URL is the one without.
func TestSPARoutesRedirectTrailingSlash(t *testing.T) {
	withDashboardShell(t, "<html>grabarr shell</html>")
	r := newWebRouter(t)

	for _, path := range []string{"/jobs/", "/settings/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

			assert.Equal(t, http.StatusMovedPermanently, rec.Code)
			assert.Equal(t, strings.TrimSuffix(path, "/"), rec.Header().Get("Location"))
		})
	}
}
