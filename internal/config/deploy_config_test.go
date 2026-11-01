package config

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The production config lives in git and Komodo deploys every push to main, so
// a broken file would only surface as a crash-looping container. Catch it here.
func TestDeployConfig(t *testing.T) {
	for _, v := range []string{"PUSHOVER_TOKEN", "PUSHOVER_USER", "RADARR_API_KEY", "SONARR_API_KEY", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET"} {
		t.Setenv(v, "test-"+v)
	}
	data, err := os.ReadFile("../../deploy/config.yaml")
	require.NoError(t, err)

	cfg, err := parseConfig(data)
	require.NoError(t, err, "strict: an unknown key in deploy/config.yaml fails here")

	require.Len(t, cfg.Remotes, 1)
	assert.Equal(t, "/config/grabarr_rsa", cfg.Remotes[0].SSHKeyFile,
		"the SSH key stays in the host config folder, outside git")
	assert.Len(t, cfg.Remotes[0].WatchedPaths, 2)
	assert.Equal(t, "/data/grabarr.db", cfg.Database.Path)
	assert.True(t, cfg.Auth.OIDC.Enabled())
	assert.False(t, cfg.Auth.DevLogin, "dev login must never be on in production")
	assert.True(t, cfg.SecureCookies())
}

// Every ${VAR} in deploy/config.yaml must be passed into the container by
// docker-compose.yml, or it silently expands to an empty string.
func TestDeployConfigVarsReachContainer(t *testing.T) {
	data, err := os.ReadFile("../../deploy/config.yaml")
	require.NoError(t, err)

	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	raw, err := os.ReadFile("../../docker-compose.yml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(raw, &compose))
	env := compose.Services["grabarr"].Environment

	var missing []string
	for _, m := range regexp.MustCompile(`\$\{(\w+)\}`).FindAllStringSubmatch(string(data), -1) {
		if _, ok := env[m[1]]; !ok {
			missing = append(missing, m[1])
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "referenced in deploy/config.yaml but not in docker-compose.yml's environment")
	assert.Equal(t, "/etc/grabarr/config.yaml", env["GRABARR_CONFIG"])
}
