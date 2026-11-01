package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/repository"
)

// newTestServer returns grabarr's auth wiring around a stub app, backed by a
// real repository with dev login on.
func newTestServer(t *testing.T) (http.Handler, *repository.Repository) {
	t.Helper()
	repo, err := repository.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })

	svc := &Service{
		store:   repo,
		cfg:     config.AuthConfig{DevLogin: true, SessionDays: 90},
		baseURL: "http://localhost:8080",
		now:     time.Now,
	}
	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		p, _ := PrincipalFrom(r.Context())
		_, _ = w.Write([]byte("app:" + p.String()))
	})
	return svc.Middleware(r), repo
}

func do(h http.Handler, method, target string, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if mod != nil {
		mod(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestMiddleware_Unauthenticated(t *testing.T) {
	h, _ := newTestServer(t)

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantLocation       string
	}{
		{"health stays open", "GET", "/api/v1/health", http.StatusOK, ""},
		{"build assets stay open", "GET", "/_app/immutable/entry/app.js", http.StatusOK, ""},
		{"the manifest stays open", "GET", "/manifest.webmanifest", http.StatusOK, ""},
		{"icons stay open", "GET", "/icon.svg", http.StatusOK, ""},
		{"other root files need auth", "GET", "/secret.txt", http.StatusFound, "/auth/login?return_to=%2Fsecret.txt"},
		{"api read needs auth", "GET", "/api/v1/jobs", http.StatusUnauthorized, ""},
		{"api write needs auth", "POST", "/api/v1/jobs", http.StatusUnauthorized, ""},
		{"metrics needs auth", "GET", "/api/v1/metrics", http.StatusUnauthorized, ""},
		{"page load goes to sign-in", "GET", "/jobs?status=failed", http.StatusFound, "/auth/login?return_to=%2Fjobs%3Fstatus%3Dfailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(h, tt.method, tt.path, nil)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantLocation != "" {
				assert.Equal(t, tt.wantLocation, w.Header().Get("Location"))
			}
			if tt.wantStatus == http.StatusUnauthorized {
				assert.JSONEq(t, `{"success":false,"error":"unauthorized"}`, w.Body.String())
			}
		})
	}
}

func TestDevLoginSession(t *testing.T) {
	h, _ := newTestServer(t)

	// Without OIDC configured, /auth/login hands over to dev login.
	w := do(h, "GET", "/auth/login?return_to=/jobs", nil)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/auth/dev-login?name=dev&return_to=%2Fjobs", w.Header().Get("Location"))

	w = do(h, "GET", "/auth/dev-login?name=Laura&return_to=/jobs", nil)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/jobs", w.Header().Get("Location"))
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	session := cookies[0]
	assert.Equal(t, sessionCookie, session.Name)
	assert.True(t, session.HttpOnly)

	withSession := func(r *http.Request) { r.AddCookie(session) }
	w = do(h, "GET", "/api/v1/jobs", withSession)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "app:Laura", w.Body.String())

	w = do(h, "GET", "/auth/me", withSession)
	assert.JSONEq(t, `{"success":true,"data":{"name":"Laura"}}`, w.Body.String())

	do(h, "GET", "/auth/logout", withSession)
	w = do(h, "GET", "/api/v1/jobs", withSession)
	assert.Equal(t, http.StatusOK, w.Code, "a GET (say, a link on another site) doesn't sign you out")
	w = do(h, "POST", "/auth/logout", withSession)
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = do(h, "GET", "/api/v1/jobs", withSession)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "the session is gone after logout")
}

func TestBearerToken(t *testing.T) {
	h, repo := newTestServer(t)
	tok := NewToken()
	require.NoError(t, repo.CreateAPIToken("scripts", HashToken(tok)))

	w := do(h, "POST", "/api/v1/jobs/1/retry", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+tok)
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "app:token:scripts", w.Body.String())

	for _, hdr := range []string{"Bearer wrong", "Bearer ", "Basic " + tok, tok} {
		w = do(h, "GET", "/api/v1/jobs", func(r *http.Request) { r.Header.Set("Authorization", hdr) })
		assert.Equal(t, http.StatusUnauthorized, w.Code, "header %q", hdr)
	}
}

func TestDevLoginOffByDefault(t *testing.T) {
	repo, err := repository.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	svc := &Service{store: repo, cfg: config.AuthConfig{
		OIDC: config.OIDCConfig{Issuer: "https://id.example", ClientID: "c", ClientSecret: "s"},
	}, now: time.Now}
	r := chi.NewRouter()
	svc.RegisterRoutes(r)

	w := do(svc.Middleware(r), "GET", "/auth/dev-login?name=x", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "dev login must not exist unless enabled")
}

func TestSafeReturnTo(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "/",
		"/jobs":                 "/jobs",
		"/jobs?status=failed":   "/jobs?status=failed",
		"https://evil.example/": "/",
		"//evil.example/":       "/",
		`/\evil.example`:        "/",
		"jobs":                  "/",
		"javascript:alert(1)":   "/",
	} {
		assert.Equal(t, want, safeReturnTo(in), "input %q", in)
	}
}

func TestHashToken(t *testing.T) {
	assert.Equal(t, HashToken("a"), HashToken("a"))
	assert.NotEqual(t, HashToken("a"), HashToken("b"))
	assert.Len(t, HashToken("a"), 64)
	assert.False(t, strings.Contains(HashToken(NewToken()), "="))
}
