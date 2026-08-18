package rsync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient() *Client {
	return NewClient("seedbox.example.com", "user", "/config/grabarr_rsa")
}

func TestBuildArgs_NoBandwidthLimit(t *testing.T) {
	args := testClient().buildArgs("/remote/file.mkv", "/local/dir", CopyOptions{})

	joined := strings.Join(args, " ")
	assert.NotContains(t, joined, "--bwlimit", "an unset limit must not throttle the transfer")
	assert.Contains(t, args, "--info=progress2")
	assert.Contains(t, args, "--partial-dir=.rsync-partial")

	// Source and destination stay last, in that order.
	require.GreaterOrEqual(t, len(args), 2)
	assert.Equal(t, "user@seedbox.example.com:/remote/file.mkv", args[len(args)-2])
	assert.Equal(t, "/local/dir", args[len(args)-1])
}

func TestBuildArgs_WithBandwidthLimit(t *testing.T) {
	args := testClient().buildArgs("/remote/file.mkv", "/local/dir", CopyOptions{BandwidthLimitKiBps: 24414})

	assert.Contains(t, args, "--bwlimit=24414")
	assert.Equal(t, "/local/dir", args[len(args)-1], "the limit must not displace the destination")
}

func TestBuildArgs_NegativeLimitIsIgnored(t *testing.T) {
	args := testClient().buildArgs("/remote/file.mkv", "/local/dir", CopyOptions{BandwidthLimitKiBps: -5})

	assert.NotContains(t, strings.Join(args, " "), "--bwlimit")
}

func TestBuildArgs_UsesConfiguredSSHKey(t *testing.T) {
	args := testClient().buildArgs("/remote/file.mkv", "/local/dir", CopyOptions{})

	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "-i /config/grabarr_rsa")
	assert.Contains(t, joined, "StrictHostKeyChecking=no")
}
