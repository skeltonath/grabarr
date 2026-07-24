// Package arr provides a minimal client for the Radarr/Sonarr v3 API.
//
// grabarr uses it to tell the *arrs when a transfer has actually landed on
// local disk. Without this, the *arrs only learn about completions by polling
// qBittorrent on the seedbox — which reports "done" when the torrent finishes
// seeding-side, long before grabarr has rsynced the file locally.
package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// commandPath is the v3 command endpoint, identical on Radarr and Sonarr.
	commandPath = "/api/v3/command"

	// cmdRefreshMonitoredDownloads asks the *arr to re-poll its download client
	// and re-attempt any queued imports. It is idempotent, which is what lets us
	// coalesce many per-file job completions into a single call.
	cmdRefreshMonitoredDownloads = "RefreshMonitoredDownloads"
)

// Client talks to a single Radarr or Sonarr instance.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient returns a Client for the instance at baseURL.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RefreshMonitoredDownloads triggers an immediate re-check of the instance's
// download queue, prompting it to retry imports that were previously skipped
// because the file was not yet present locally.
func (c *Client) RefreshMonitoredDownloads(ctx context.Context) error {
	return c.postCommand(ctx, cmdRefreshMonitoredDownloads)
}

func (c *Client) postCommand(ctx context.Context, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("marshal command %s: %w", name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+commandPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request for command %s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post command %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("command %s returned status %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
