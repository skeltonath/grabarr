package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skeltonath/grabarr/internal/api"
	"github.com/skeltonath/grabarr/internal/arr"
	"github.com/skeltonath/grabarr/internal/auth"
	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/executor"
	"github.com/skeltonath/grabarr/internal/gatekeeper"
	"github.com/skeltonath/grabarr/internal/interfaces"
	"github.com/skeltonath/grabarr/internal/notifications"
	"github.com/skeltonath/grabarr/internal/queue"
	"github.com/skeltonath/grabarr/internal/repository"
	internalsync "github.com/skeltonath/grabarr/internal/sync"

	"github.com/go-chi/chi/v5"
)

func main() {
	// Setup logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "token":
			os.Exit(tokenCommand(os.Args[2:], os.Stdout, os.Stderr))
		}
	}

	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

// healthcheck is the container's HEALTHCHECK: the image has no curl or wget.
func healthcheck() int {
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://127.0.0.1:8080/api/v1/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy: status", resp.StatusCode)
		return 1
	}
	return 0
}

func run() error {
	// Load configuration
	configPath := getConfigPath()
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	slog.Info("configuration loaded", "config_path", configPath)

	// Update logging based on config
	setupLogging(cfg.GetLogging())

	// Initialize database
	repo, err := repository.New(cfg.GetDatabase().Path)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer repo.Close()

	slog.Info("database initialized", "path", cfg.GetDatabase().Path)

	// Settings changed from the web UI live in the database, layered over
	// config.yaml. Attaching the store restores whatever was set last run.
	if err := cfg.AttachSettingsStore(repo); err != nil {
		return fmt.Errorf("failed to load runtime settings: %w", err)
	}

	// Initialize gatekeeper
	gk := gatekeeper.New(cfg)
	if err := gk.Start(); err != nil {
		return fmt.Errorf("failed to start gatekeeper: %w", err)
	}
	defer gk.Stop()

	// Initialize notifications
	notifier := notifications.NewPushoverNotifier(cfg)

	// Initialize job queue
	jobQueue := queue.New(repo, cfg, gk, notifier)

	// Initialize job executor (using rsync as default)
	jobExecutor := executor.NewRsyncExecutor(cfg, gk, repo)
	jobQueue.SetJobExecutor(jobExecutor)

	// Start job queue and executor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the progress monitor
	jobExecutor.Start(ctx)
	defer jobExecutor.Stop()

	if err := jobQueue.Start(ctx); err != nil {
		return fmt.Errorf("failed to start job queue: %w", err)
	}

	// Send startup notification
	if notifier.IsEnabled() {
		slog.Info("sending startup notification")
		if err := notifier.NotifySystemAlert(
			"Service Started",
			"Grabarr has started successfully and is ready to process jobs.",
			0, // Normal priority
		); err != nil {
			slog.Warn("failed to send startup notification", "error", err)
		}
	}

	// Initialize and start the sync scanner
	scanner := internalsync.New(cfg, repo, jobQueue)

	// The scanner is the queue's source of truth for which archive volumes
	// actually exist, so extraction never runs against a partial set.
	jobQueue.SetRemoteVolumeLister(scanner)

	// Tell Radarr/Sonarr when a transfer lands locally. Without this they only
	// learn about completions by polling qBittorrent on the seedbox, which
	// reports "done" long before the file exists on this machine.
	if importNotifier := newImportNotifier(cfg); importNotifier != nil {
		jobQueue.SetImportNotifier(importNotifier)
	}

	scanner.Start(ctx)

	// Setup HTTP server
	router := chi.NewRouter()

	// Everything but the health check needs a Pocket ID session or an API token.
	authService := auth.New(repo, cfg)
	authService.RegisterRoutes(router)

	// Setup API handlers
	handlers := api.NewHandlers(jobQueue, gk, cfg, repo, scanner)
	handlers.RegisterRoutes(router)

	// Log registered routes for debugging
	slog.Info("routes registered", "web_ui_available", "check /dashboard and /ui endpoints")

	// Create HTTP server
	serverConfig := cfg.GetServer()
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port),
		Handler:      authService.Middleware(router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in goroutine
	go func() {
		slog.Info("starting HTTP server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Watch for configuration changes
	go func() {
		configChanges := cfg.WatchForChanges()
		for {
			select {
			case <-ctx.Done():
				return
			case <-configChanges:
				slog.Info("configuration changed, updating logging")
				setupLogging(cfg.GetLogging())
				// Note: Other components should also watch for config changes
				// and update themselves accordingly
			}
		}
	}()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	slog.Info("shutdown signal received, initiating graceful shutdown")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverConfig.ShutdownTimeout)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	// Stop job queue (marks jobs as queued)
	if err := jobQueue.Stop(); err != nil {
		slog.Error("job queue shutdown error", "error", err)
	}

	// Cancel main context
	cancel()

	// Send final notification if any jobs were interrupted
	jobSummary, jobErr := jobQueue.GetSummary()

	interruptedJobs := 0

	if jobErr == nil {
		interruptedJobs = jobSummary.QueuedJobs
	}

	if interruptedJobs > 0 && notifier.IsEnabled() {
		message := fmt.Sprintf("Grabarr is shutting down. %d job(s) have been queued for restart.",
			interruptedJobs)
		notifier.NotifySystemAlert(
			"Service Shutdown",
			message,
			1, // High priority
		)
	}

	slog.Info("shutdown completed")
	return nil
}

func getConfigPath() string {
	if configPath := os.Getenv("GRABARR_CONFIG"); configPath != "" {
		return configPath
	}

	// Try common paths
	candidates := []string{
		"/config/config.yaml",
		"./config.yaml",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	slog.Error("no configuration file found")
	return ""
}

func setupLogging(logConfig config.LoggingConfig) {
	var level slog.Level
	switch logConfig.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
	}

	if logConfig.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// newImportNotifier builds the Radarr/Sonarr notifier from config, or returns
// nil when no usable instance is configured.
func newImportNotifier(cfg *config.Config) interfaces.ImportNotifier {
	mm := cfg.GetMediaManagers()

	instances := make([]arr.InstanceConfig, 0, len(mm.Instances))
	for _, i := range mm.Instances {
		instances = append(instances, arr.InstanceConfig{
			Name:       i.Name,
			Enabled:    i.Enabled,
			URL:        i.URL,
			APIKey:     i.APIKey,
			Categories: i.Categories,
		})
	}

	notifier := arr.NewNotifier(instances, mm.Debounce)
	if !notifier.HasTargets() {
		slog.Info("no media managers configured, import notifications disabled")
		return nil
	}

	slog.Info("import notifications enabled", "debounce", mm.Debounce)
	return notifier
}
