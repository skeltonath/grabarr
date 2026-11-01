package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertUser_CreatesThenRefreshes(t *testing.T) {
	repo := setupTestRepo(t)

	first, err := repo.UpsertUser("https://id.example", "sub-1", "d@example.com", "David")
	require.NoError(t, err)
	again, err := repo.UpsertUser("https://id.example", "sub-1", "new@example.com", "Dave")
	require.NoError(t, err)

	assert.Equal(t, first.ID, again.ID, "same issuer and subject is the same person")
	assert.Equal(t, "new@example.com", again.Email)
	assert.Equal(t, "Dave", again.Name)

	other, err := repo.UpsertUser("dev", "sub-1", "", "Dev")
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, other.ID, "a different issuer is a different user")
}

func TestSessions(t *testing.T) {
	repo := setupTestRepo(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	u, err := repo.UpsertUser("dev", "laura", "", "Laura")
	require.NoError(t, err)

	require.NoError(t, repo.CreateSession("live", u.ID, now.Add(time.Hour)))
	require.NoError(t, repo.CreateSession("stale", u.ID, now.Add(-time.Hour)))

	got, err := repo.UserBySession("live", now)
	require.NoError(t, err)
	assert.Equal(t, "Laura", got.Name)

	_, err = repo.UserBySession("stale", now)
	assert.ErrorIs(t, err, ErrNotFound, "expired sessions don't authenticate")
	_, err = repo.UserBySession("missing", now)
	assert.ErrorIs(t, err, ErrNotFound)

	n, err := repo.PurgeExpiredSessions(now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	require.NoError(t, repo.DeleteSession("live"))
	_, err = repo.UserBySession("live", now)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAPITokens(t *testing.T) {
	repo := setupTestRepo(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	require.NoError(t, repo.CreateAPIToken("scripts", "hash-1"))
	assert.Error(t, repo.CreateAPIToken("scripts", "hash-2"), "names are unique")

	tokens, err := repo.ListAPITokens()
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Nil(t, tokens[0].LastUsedAt)

	name, err := repo.APITokenByHash("hash-1", now)
	require.NoError(t, err)
	assert.Equal(t, "scripts", name)

	tokens, err = repo.ListAPITokens()
	require.NoError(t, err)
	require.NotNil(t, tokens[0].LastUsedAt)
	assert.True(t, tokens[0].LastUsedAt.Equal(now))

	_, err = repo.APITokenByHash("nope", now)
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, repo.DeleteAPIToken("scripts"))
	assert.ErrorIs(t, repo.DeleteAPIToken("scripts"), ErrNotFound)
	_, err = repo.APITokenByHash("hash-1", now)
	assert.ErrorIs(t, err, ErrNotFound, "revoked tokens stop working")
}
