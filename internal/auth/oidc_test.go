package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/repository"
)

// fakeIssuer is just enough of an OIDC provider for the code flow: discovery,
// keys, and a token endpoint that signs an id_token for the last nonce seen.
type fakeIssuer struct {
	*httptest.Server
	key   *rsa.PrivateKey
	nonce string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.URL,
			"authorization_endpoint":                f.URL + "/authorize",
			"token_endpoint":                        f.URL + "/token",
			"jwks_uri":                              f.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("code") != "good-code" || r.FormValue("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
			(&jose.SignerOptions{}).WithHeader("kid", "k1"))
		require.NoError(t, err)
		raw, err := jwt.Signed(signer).Claims(jwt.Claims{
			Issuer: f.URL, Subject: "sub-laura", Audience: jwt.Audience{"grabarr"},
			IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}).Claims(map[string]any{"nonce": f.nonce, "email": "laura@example.com", "given_name": "Laura"}).Serialize()
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": raw,
		})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestOIDCSignIn(t *testing.T) {
	issuer := newFakeIssuer(t)
	repo, err := repository.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer repo.Close()

	svc := &Service{
		store: repo,
		cfg: config.AuthConfig{SessionDays: 90, OIDC: config.OIDCConfig{
			Issuer: issuer.URL, ClientID: "grabarr", ClientSecret: "secret",
		}},
		baseURL: "https://grabarr.example",
		secure:  true,
		now:     time.Now,
	}
	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	r.Get("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		p, _ := PrincipalFrom(r.Context())
		_, _ = w.Write([]byte(p.String()))
	})
	h := svc.Middleware(r)

	// 1. Login sends the browser to Pocket ID with PKCE and a nonce.
	w := do(h, "GET", "/auth/login?return_to=/jobs", nil)
	require.Equal(t, http.StatusFound, w.Code)
	authURL, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	q := authURL.Query()
	assert.Equal(t, issuer.URL+"/authorize", authURL.Scheme+"://"+authURL.Host+authURL.Path)
	assert.Equal(t, "grabarr", q.Get("client_id"))
	assert.Equal(t, "https://grabarr.example/auth/callback", q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("nonce"))
	issuer.nonce = q.Get("nonce")
	flow := w.Result().Cookies()[0]
	assert.True(t, flow.Secure)

	// 2. A callback with the wrong state is refused.
	w = do(h, "GET", "/auth/callback?code=good-code&state=forged", func(r *http.Request) { r.AddCookie(flow) })
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Pocket ID refusing (user not in the household group) is reported.
	w = do(h, "GET", "/auth/callback?error=access_denied&state="+q.Get("state"), func(r *http.Request) { r.AddCookie(flow) })
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 4. A bad code fails the exchange.
	w = do(h, "GET", "/auth/callback?code=bad&state="+q.Get("state"), func(r *http.Request) { r.AddCookie(flow) })
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 5. The real callback creates the user and a session, then returns to /jobs.
	w = do(h, "GET", "/auth/callback?code=good-code&state="+q.Get("state"), func(r *http.Request) { r.AddCookie(flow) })
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	assert.Equal(t, "/jobs", w.Header().Get("Location"))
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	require.NotNil(t, session)
	assert.True(t, session.Secure)

	w = do(h, "GET", "/api/v1/jobs", func(r *http.Request) { r.AddCookie(session) })
	assert.Equal(t, "Laura", w.Body.String())

	// 6. A replayed id_token with a stale nonce is rejected.
	issuer.nonce = "stale"
	w = do(h, "GET", "/auth/callback?code=good-code&state="+q.Get("state"), func(r *http.Request) { r.AddCookie(flow) })
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestOIDCProviderDown(t *testing.T) {
	repo, err := repository.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	svc := &Service{store: repo, now: time.Now, cfg: config.AuthConfig{OIDC: config.OIDCConfig{
		Issuer: "http://127.0.0.1:1", ClientID: "grabarr", ClientSecret: "s",
	}}}
	r := chi.NewRouter()
	svc.RegisterRoutes(r)

	w := do(svc.Middleware(r), "GET", "/auth/login", nil)
	assert.Equal(t, http.StatusBadGateway, w.Code, "a Pocket ID outage is a clear error, not a crash")
}
