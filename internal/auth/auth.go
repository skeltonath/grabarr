// Package auth handles sign-in through the household OIDC provider (Pocket ID),
// browser sessions and API tokens for scripts. Grabarr never stores passwords;
// the Pocket ID client itself limits sign-in to the household group.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"

	"grabarr/internal/config"
	"grabarr/internal/repository"
)

const (
	sessionCookie = "grabarr_session"
	flowCookie    = "grabarr_oidc"
	// DevIssuer is the issuer recorded for users created through dev login.
	DevIssuer = "dev"
)

// Store is the slice of the repository that auth needs.
type Store interface {
	UpsertUser(issuer, subject, email, name string) (repository.User, error)
	CreateSession(tokenHash string, userID int64, expires time.Time) error
	DeleteSession(tokenHash string) error
	UserBySession(tokenHash string, now time.Time) (repository.User, error)
	PurgeExpiredSessions(now time.Time) (int64, error)
	APITokenByHash(tokenHash string, now time.Time) (string, error)
}

type Service struct {
	store   Store
	cfg     config.AuthConfig
	baseURL string
	secure  bool
	now     func() time.Time

	// The provider is discovered lazily so grabarr starts (and keeps copying)
	// while Pocket ID is down; a failed discovery is retried on the next sign-in.
	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

func New(store Store, cfg *config.Config) *Service {
	return &Service{
		store:   store,
		cfg:     cfg.GetAuth(),
		baseURL: strings.TrimSuffix(cfg.GetServer().BaseURL, "/"),
		secure:  cfg.SecureCookies(),
		now:     time.Now,
	}
}

// Principal is who made a request: a signed-in person or a named API token.
type Principal struct {
	User  *repository.User
	Token string
}

func (p Principal) String() string {
	if p.User != nil {
		return p.User.Name
	}
	return "token:" + p.Token
}

type ctxKey struct{}

// PrincipalFrom returns who made the request, as placed in ctx by Middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// public reports whether a path is reachable without signing in: the sign-in
// flow itself, static assets, and the health check that the container
// healthcheck and Glance poll.
func public(path string) bool {
	return path == "/api/v1/health" ||
		strings.HasPrefix(path, "/auth/") ||
		strings.HasPrefix(path, "/static/")
}

// Middleware requires a session cookie or an API token on everything except
// public paths. API requests without one get a 401; page loads are sent to
// sign in and brought back afterwards.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		p, err := s.authenticate(r)
		if err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				slog.Error("authenticate", "error", err)
			}
			if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
				http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

func (s *Service) authenticate(r *http.Request) (Principal, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || tok == "" {
			return Principal{}, repository.ErrNotFound
		}
		name, err := s.store.APITokenByHash(HashToken(tok), s.now())
		if err != nil {
			return Principal{}, err
		}
		return Principal{Token: name}, nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return Principal{}, repository.ErrNotFound
	}
	u, err := s.store.UserBySession(HashToken(c.Value), s.now())
	if err != nil {
		return Principal{}, err
	}
	return Principal{User: &u}, nil
}

// RegisterRoutes mounts the sign-in flow under /auth.
func (s *Service) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/auth/login", s.handleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", s.handleCallback).Methods("GET")
	r.HandleFunc("/auth/logout", s.handleLogout).Methods("POST")
	r.HandleFunc("/auth/me", s.handleMe).Methods("GET")
	if s.cfg.DevLogin {
		r.HandleFunc("/auth/dev-login", s.handleDevLogin).Methods("GET")
	}
}

func (s *Service) oidcClient(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth != nil {
		return s.oauth, s.verifier, nil
	}
	o := s.cfg.OIDC
	provider, err := oidc.NewProvider(ctx, o.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("discover %s: %w", o.Issuer, err)
	}
	s.oauth = &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.baseURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	s.verifier = provider.Verifier(&oidc.Config{ClientID: o.ClientID})
	return s.oauth, s.verifier, nil
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnTo := safeReturnTo(r.URL.Query().Get("return_to"))
	if !s.cfg.OIDC.Enabled() {
		// Local preview: dev login is the only way in.
		http.Redirect(w, r, "/auth/dev-login?name=dev&return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	oc, _, err := s.oidcClient(r.Context())
	if err != nil {
		slog.Error("oidc discovery", "error", err)
		http.Error(w, "Pocket ID is unavailable, try again shortly", http.StatusBadGateway)
		return
	}
	state, nonce, verifier := randomToken(), randomToken(), oauth2.GenerateVerifier()
	// The flow cookie carries state, nonce and PKCE verifier across the redirect.
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookie,
		Value:    strings.Join([]string{state, nonce, verifier, returnTo}, "|"),
		Path:     "/auth",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, oc.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(flowCookie)
	var parts []string
	if err == nil {
		parts = strings.SplitN(c.Value, "|", 4)
	}
	if len(parts) != 4 || r.URL.Query().Get("state") != parts[0] {
		http.Error(w, "sign-in expired or invalid, try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/auth", MaxAge: -1})
	nonce, verifier, returnTo := parts[1], parts[2], parts[3]

	if e := r.URL.Query().Get("error"); e != "" {
		// Pocket ID says access_denied when the user isn't in an allowed group.
		http.Error(w, "sign-in failed: "+e, http.StatusUnauthorized)
		return
	}
	oc, idv, err := s.oidcClient(r.Context())
	if err != nil {
		http.Error(w, "Pocket ID is unavailable, try again shortly", http.StatusBadGateway)
		return
	}
	tok, err := oc.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Warn("oidc code exchange", "error", err)
		http.Error(w, "sign-in failed", http.StatusUnauthorized)
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "sign-in failed: no id_token", http.StatusUnauthorized)
		return
	}
	idt, err := idv.Verify(r.Context(), raw)
	if err != nil || idt.Nonce != nonce {
		slog.Warn("oidc id_token rejected", "error", err)
		http.Error(w, "sign-in failed", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		GivenName         string `json:"given_name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = idt.Claims(&claims)
	name := firstNonEmpty(claims.GivenName, claims.Name, claims.PreferredUsername, claims.Email)

	u, err := s.store.UpsertUser(idt.Issuer, idt.Subject, claims.Email, name)
	if err != nil {
		slog.Error("upsert user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.startSession(w, r, u, returnTo)
}

func (s *Service) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	u, err := s.store.UpsertUser(DevIssuer, strings.ToLower(name), "", name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	slog.Warn("dev login used", "user", name)
	s.startSession(w, r, u, safeReturnTo(r.URL.Query().Get("return_to")))
}

func (s *Service) startSession(w http.ResponseWriter, r *http.Request, u repository.User, returnTo string) {
	tok := randomToken()
	ttl := time.Duration(s.cfg.SessionDays) * 24 * time.Hour
	if err := s.store.CreateSession(HashToken(tok), u.ID, s.now().Add(ttl)); err != nil {
		slog.Error("create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n, err := s.store.PurgeExpiredSessions(s.now()); err == nil && n > 0 {
		slog.Info("purged expired sessions", "count", n)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	slog.Info("sign-in", "user_id", u.ID, "name", u.Name)
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// handleLogout ends the session. It's POST-only, so a link on another site
// can't sign people out.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the signed-in user's name for the page header, or 401.
func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p, err := s.authenticate(r)
	w.Header().Set("Content-Type", "application/json")
	if err != nil || p.User == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]string{"name": p.User.Name}})
}

// HashToken is how session cookies and API tokens are stored: never in the clear.
func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// NewToken returns a random URL-safe token.
func NewToken() string { return randomToken() }

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// safeReturnTo only allows same-origin paths, to avoid open redirects.
func safeReturnTo(p string) string {
	u, err := url.Parse(p)
	if p == "" || err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, `\`) {
		return "/"
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
