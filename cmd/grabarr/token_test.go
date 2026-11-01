package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/auth"
	"github.com/skeltonath/grabarr/internal/repository"
)

func TestTokenCommand(t *testing.T) {
	repo, err := repository.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer repo.Close()

	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runTokenCommand(repo, args, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	code, out, _ := run("create", "scripts")
	require.Equal(t, 0, code)
	tok := strings.TrimSpace(out)
	name, err := repo.APITokenByHash(auth.HashToken(tok), time.Now())
	require.NoError(t, err, "the printed token is the one stored (as a hash)")
	assert.Equal(t, "scripts", name)

	code, _, errOut := run("create", "scripts")
	assert.Equal(t, 1, code)
	assert.NotEmpty(t, errOut, "duplicate names are rejected")

	code, out, _ = run("list")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "scripts")
	assert.NotContains(t, out, tok, "list never shows the token")

	code, out, _ = run("revoke", "scripts")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "revoked scripts")

	code, _, errOut = run("revoke", "scripts")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, `no token named "scripts"`)

	for _, args := range [][]string{nil, {"create"}, {"bogus"}, {"list", "extra"}} {
		code, _, errOut = run(args...)
		assert.Equal(t, 2, code, "args %v", args)
		assert.Contains(t, errOut, "usage:")
	}
}
