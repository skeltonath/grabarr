package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/models"
)

type PushoverNotifier struct {
	config     *config.Config
	httpClient *http.Client
	enabled    bool
	apiURL     string
}

type pushoverRequest struct {
	Token     string `json:"token"`
	User      string `json:"user"`
	Message   string `json:"message"`
	Title     string `json:"title,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	URL       string `json:"url,omitempty"`
	URLTitle  string `json:"url_title,omitempty"`
	Device    string `json:"device,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Sound     string `json:"sound,omitempty"`
	Retry     int    `json:"retry,omitempty"`
	Expire    int    `json:"expire,omitempty"`
}

type pushoverResponse struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Errors  []string `json:"errors,omitempty"`
	Receipt string   `json:"receipt,omitempty"`
}

const pushoverAPIURL = "https://api.pushover.net/1/messages.json"

func NewPushoverNotifier(cfg *config.Config) *PushoverNotifier {
	return &PushoverNotifier{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		enabled: cfg.GetNotifications().Pushover.Enabled,
		apiURL:  pushoverAPIURL,
	}
}

func (p *PushoverNotifier) IsEnabled() bool {
	return p.enabled
}

func (p *PushoverNotifier) NotifyJobFailed(job *models.Job) error {
	if !p.enabled {
		return nil
	}

	cfg := p.config.GetNotifications().Pushover

	title := fmt.Sprintf("Grabarr Job Failed: %s", job.Name)
	message := p.buildJobFailedMessage(job)

	req := pushoverRequest{
		Token:     cfg.Token,
		User:      cfg.User,
		Message:   message,
		Title:     title,
		Priority:  cfg.Priority,
		Timestamp: time.Now().Unix(),
		Sound:     "falling", // Default sound for failures
	}

	// Use higher priority for failed jobs that have exhausted retries
	if job.Retries >= job.MaxRetries {
		req.Priority = 1 // High priority
		req.Sound = "siren"
	}

	// If priority is 2 (emergency), set retry and expire
	if req.Priority == 2 {
		req.Retry = int(cfg.RetryInterval.Seconds())
		req.Expire = int(cfg.ExpireTime.Seconds())
	}

	return p.sendNotification(req)
}

func (p *PushoverNotifier) NotifyJobCompleted(job *models.Job) error {
	if !p.enabled {
		return nil
	}

	// Only notify for important jobs or if explicitly requested
	// You might want to add configuration for this
	if job.Priority < 5 {
		return nil
	}

	cfg := p.config.GetNotifications().Pushover

	title := fmt.Sprintf("Grabarr Job Completed: %s", job.Name)
	message := p.buildJobCompletedMessage(job)

	req := pushoverRequest{
		Token:     cfg.Token,
		User:      cfg.User,
		Message:   message,
		Title:     title,
		Priority:  -1, // Low priority for completions
		Timestamp: time.Now().Unix(),
		Sound:     "none", // Silent for completions
	}

	return p.sendNotification(req)
}

func (p *PushoverNotifier) NotifySystemAlert(title, message string, priority int) error {
	if !p.enabled {
		return nil
	}

	cfg := p.config.GetNotifications().Pushover

	req := pushoverRequest{
		Token:     cfg.Token,
		User:      cfg.User,
		Message:   message,
		Title:     fmt.Sprintf("Grabarr Alert: %s", title),
		Priority:  priority,
		Timestamp: time.Now().Unix(),
		Sound:     "pushover", // Default sound
	}

	// Adjust sound based on priority
	switch priority {
	case -2:
		req.Sound = "none"
	case -1:
		req.Sound = "none"
	case 0:
		req.Sound = "pushover"
	case 1:
		req.Sound = "persistent"
	case 2:
		req.Sound = "siren"
		req.Retry = int(cfg.RetryInterval.Seconds())
		req.Expire = int(cfg.ExpireTime.Seconds())
	}

	return p.sendNotification(req)
}

func (p *PushoverNotifier) sendNotification(req pushoverRequest) error {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal pushover request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "grabarr/1.0")

	slog.Debug("sending pushover notification",
		"title", req.Title,
		"priority", req.Priority)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send pushover notification: %w", err)
	}
	defer resp.Body.Close()

	var pushoverResp pushoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&pushoverResp); err != nil {
		return fmt.Errorf("failed to decode pushover response: %w", err)
	}

	if pushoverResp.Status != 1 {
		return fmt.Errorf("pushover API error: %s", strings.Join(pushoverResp.Errors, ", "))
	}

	slog.Info("pushover notification sent successfully",
		"request_id", pushoverResp.Request,
		"receipt", pushoverResp.Receipt)

	return nil
}

func (p *PushoverNotifier) buildJobFailedMessage(job *models.Job) string {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("Job: %s\n", job.Name))
	msg.WriteString(fmt.Sprintf("Remote Path: %s\n", job.RemotePath))
	msg.WriteString(fmt.Sprintf("Status: %s\n", job.Status))
	msg.WriteString(fmt.Sprintf("Retry: %d/%d\n", job.Retries, job.MaxRetries))

	if job.ErrorMessage != "" {
		msg.WriteString(fmt.Sprintf("Error: %s\n", job.ErrorMessage))
	}

	if job.StartedAt != nil {
		duration := time.Since(*job.StartedAt)
		msg.WriteString(fmt.Sprintf("Duration: %s\n", duration.Round(time.Second)))
	}

	if job.Progress.TransferredBytes > 0 && job.Progress.TotalBytes > 0 {
		msg.WriteString(fmt.Sprintf("Progress: %.1f%% (%s/%s)\n",
			job.Progress.Percentage,
			formatBytes(job.Progress.TransferredBytes),
			formatBytes(job.Progress.TotalBytes)))
	}

	if job.Metadata.Category != "" {
		msg.WriteString(fmt.Sprintf("Category: %s\n", job.Metadata.Category))
	}

	msg.WriteString(fmt.Sprintf("Job ID: %d", job.ID))

	return msg.String()
}

func (p *PushoverNotifier) buildJobCompletedMessage(job *models.Job) string {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("Job: %s\n", job.Name))
	msg.WriteString(fmt.Sprintf("Remote Path: %s\n", job.RemotePath))

	if job.StartedAt != nil && job.CompletedAt != nil {
		duration := job.CompletedAt.Sub(*job.StartedAt)
		msg.WriteString(fmt.Sprintf("Duration: %s\n", duration.Round(time.Second)))
	}

	if job.Progress.TotalBytes > 0 {
		msg.WriteString(fmt.Sprintf("Size: %s\n", formatBytes(job.Progress.TotalBytes)))
	}

	if job.Progress.TransferSpeed > 0 {
		msg.WriteString(fmt.Sprintf("Avg Speed: %s/s\n", formatBytes(job.Progress.TransferSpeed)))
	}

	if job.Metadata.Category != "" {
		msg.WriteString(fmt.Sprintf("Category: %s\n", job.Metadata.Category))
	}

	msg.WriteString(fmt.Sprintf("Job ID: %d", job.ID))

	return msg.String()
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
