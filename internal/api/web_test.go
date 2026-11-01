package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/mocks"
)

func newWebRouter(t *testing.T) chi.Router {
	t.Helper()
	h := NewHandlers(mocks.NewMockJobQueue(t), mocks.NewMockGatekeeper(t), &config.Config{}, nil, nil)
	h.ui = fstest.MapFS{
		"index.html":                  {Data: []byte("<html>grabarr app</html>")},
		"icon.svg":                    {Data: []byte("<svg/>")},
		"_app/immutable/entry/app.js": {Data: []byte("console.log(1)")},
	}
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func get(r http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// Every client-side route serves the app, so a deep link or a refresh on
// /jobs?job=7 loads it instead of a 404.
func TestWebUI_PagesServeTheApp(t *testing.T) {
	r := newWebRouter(t)
	for _, path := range []string{"/", "/jobs", "/jobs?status=failed&job=7", "/settings", "/jobs/"} {
		rec := get(r, "GET", path)
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Contains(t, rec.Body.String(), "grabarr app", path)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"), path)
	}
}

func TestWebUI_Assets(t *testing.T) {
	r := newWebRouter(t)

	rec := get(r, "GET", "/_app/immutable/entry/app.js")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable", "hashed build files cache forever")

	rec = get(r, "GET", "/icon.svg")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.NotEmpty(t, rec.Header().Get("ETag"))

	rec = get(r, "GET", "/_app/immutable/missing.js")
	assert.Equal(t, http.StatusNotFound, rec.Code, "a missing asset is a 404, not the app's HTML")
}

func TestWebUI_LeavesTheAPIAlone(t *testing.T) {
	r := newWebRouter(t)

	rec := get(r, "GET", "/api/v1/nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "grabarr app")

	rec = get(r, "PUT", "/api/v1/settings")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = get(r, "POST", "/jobs")
	assert.Equal(t, http.StatusNotFound, rec.Code, "pages are GET only")
}
