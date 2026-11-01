package api

import (
	"net/http"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/mocks"
)

// The route table is the API's contract with the UI and the scripts; this
// pins it so a router change can't silently drop or rename an endpoint.
func TestRouteTable(t *testing.T) {
	h := NewHandlers(mocks.NewMockJobQueue(t), mocks.NewMockGatekeeper(t), &config.Config{}, nil, nil)
	h.ui = fstest.MapFS{}
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	var got []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}))
	sort.Strings(got)

	want := []string{
		"DELETE /api/v1/jobs/{id:[0-9]+}",
		"GET /api/v1/health",
		"GET /api/v1/jobs",
		"GET /api/v1/jobs/summary",
		"GET /api/v1/jobs/{id:[0-9]+}",
		"GET /api/v1/metrics",
		"GET /api/v1/remote-files",
		"GET /api/v1/remote-files/tree",
		"GET /api/v1/settings",
		"GET /api/v1/status",
		"GET /api/v1/sync/status",
		"PATCH /api/v1/settings",
		"POST /api/v1/jobs",
		"POST /api/v1/jobs/{id:[0-9]+}/cancel",
		"POST /api/v1/jobs/{id:[0-9]+}/retry",
		"POST /api/v1/jobs/{id:[0-9]+}/start-now",
		"POST /api/v1/remote-files/queue-folder",
		"POST /api/v1/remote-files/{id:[0-9]+}/ignore",
		"POST /api/v1/remote-files/{id:[0-9]+}/queue",
		"POST /api/v1/remote-files/{id:[0-9]+}/restore",
		"POST /api/v1/sync/scan",
	}
	// The web UI is the router's NotFound handler (web.go), not a route.
	assert.Equal(t, want, got)
}
