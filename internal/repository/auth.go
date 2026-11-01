package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a session, token or user doesn't exist (or a
// session has expired).
var ErrNotFound = errors.New("not found")

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type APIToken struct {
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// UpsertUser returns the user for (issuer, subject), creating it on first
// sign-in and refreshing email and name on later ones.
func (r *Repository) UpsertUser(issuer, subject, email, name string) (User, error) {
	var u User
	err := r.db.QueryRow(`
		INSERT INTO users (oidc_issuer, oidc_subject, email, name) VALUES (?, ?, ?, ?)
		ON CONFLICT (oidc_issuer, oidc_subject) DO UPDATE SET
			email = excluded.email,
			name  = excluded.name
		RETURNING id, email, name`,
		issuer, subject, email, name,
	).Scan(&u.ID, &u.Email, &u.Name)
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

func (r *Repository) CreateSession(tokenHash string, userID int64, expires time.Time) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, userID, expires.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSession(tokenHash string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// UserBySession returns the session's user if the session exists and hasn't expired.
func (r *Repository) UserBySession(tokenHash string, now time.Time) (User, error) {
	var u User
	err := r.db.QueryRow(`
		SELECT u.id, u.email, u.name
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, now.UTC().Format(time.RFC3339),
	).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (r *Repository) PurgeExpiredSessions(now time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CreateAPIToken stores a token's hash under a unique name.
func (r *Repository) CreateAPIToken(name, tokenHash string) error {
	_, err := r.db.Exec(`INSERT INTO api_tokens (name, token_hash) VALUES (?, ?)`, name, tokenHash)
	if err != nil {
		return fmt.Errorf("create api token %q: %w", name, err)
	}
	return nil
}

// APITokenByHash returns the token's name and records that it was used.
func (r *Repository) APITokenByHash(tokenHash string, now time.Time) (string, error) {
	var name string
	err := r.db.QueryRow(
		`UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ? RETURNING name`,
		now.UTC().Format(time.RFC3339), tokenHash,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

func (r *Repository) ListAPITokens() ([]APIToken, error) {
	rows, err := r.db.Query(`SELECT name, created_at, last_used_at FROM api_tokens ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()

	var out []APIToken
	for rows.Next() {
		var t APIToken
		var created string
		var used sql.NullString
		if err := rows.Scan(&t.Name, &created, &used); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.DateTime, created)
		if used.Valid {
			if ts, err := time.Parse(time.RFC3339, used.String); err == nil {
				t.LastUsedAt = &ts
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token by name.
func (r *Repository) DeleteAPIToken(name string) error {
	res, err := r.db.Exec(`DELETE FROM api_tokens WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete api token %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
