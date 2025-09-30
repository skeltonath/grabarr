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

	"grabarr/internal/api"
	"grabarr/internal/config"
	"grabarr/internal/executor"
	"grabarr/internal/gatekeeper"
	"grabarr/internal/notifications"
	"grabarr/internal/queue"
	"grabarr/internal/rclone"
	"grabarr/internal/repository"
	"grabarr/internal/services"

	"github.com/gorilla/mux"
)

func main() {
	// Setup logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
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

	// Initialize RClone client
	rcloneConfig := cfg.GetRClone()
	slog.Info("initializing RClone client", "daemon_addr", rcloneConfig.DaemonAddr)
	rcloneClient := rclone.NewClient(fmt.Sprintf("http://%s", rcloneConfig.DaemonAddr))

	// Initialize gatekeeper
	gk := gatekeeper.New(cfg, repo, rcloneClient)
	if err := gk.Start(); err != nil {
		return fmt.Errorf("failed to start gatekeeper: %w", err)
	}
	defer gk.Stop()

	// Initialize job queue
	jobQueue := queue.New(repo, cfg, gk)

	// Initialize job executor
	jobExecutor := executor.NewRCloneExecutor(cfg, gk, repo)
	jobQueue.SetJobExecutor(jobExecutor)

	// Initialize notifications
	notifier := notifications.NewPushoverNotifier(cfg)

	// Test notification on startup if enabled
	if notifier.IsEnabled() {
		slog.Info("testing pushover notification")
		if err := notifier.TestNotification(); err != nil {
			slog.Warn("pushover test notification failed", "error", err)
		} else {
			slog.Info("pushover test notification sent successfully")
		}
	}

	// Initialize sync service
	syncService := services.NewSyncService(cfg, repo, gk)
	slog.Info("sync service initialized")

	// Recover interrupted syncs
	if err := syncService.RecoverInterruptedSyncs(); err != nil {
		slog.Warn("failed to recover interrupted syncs", "error", err)
	}

	// Start job queue
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := jobQueue.Start(ctx); err != nil {
		return fmt.Errorf("failed to start job queue: %w", err)
	}

	// Setup HTTP server
	router := mux.NewRouter()

	// Setup API handlers
	handlers := api.NewHandlers(jobQueue, gk, cfg, syncService)
	handlers.RegisterRoutes(router)

	// Log registered routes for debugging
	slog.Info("routes registered", "web_ui_available", "check /dashboard and /ui endpoints")

	// Create HTTP server
	serverConfig := cfg.GetServer()
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port),
		Handler:      router,
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

	// Stop sync service first (marks syncs as queued)
	if err := syncService.Shutdown(); err != nil {
		slog.Error("sync service shutdown error", "error", err)
	}

	// Stop job queue (marks jobs as queued)
	if err := jobQueue.Stop(); err != nil {
		slog.Error("job queue shutdown error", "error", err)
	}

	// Cancel main context
	cancel()

	// Send final notification if any jobs were interrupted
	jobSummary, jobErr := jobQueue.GetSummary()
	syncSummary, syncErr := syncService.GetSyncSummary()

	interruptedJobs := 0
	interruptedSyncs := 0

	if jobErr == nil {
		interruptedJobs = jobSummary.QueuedJobs
	}
	if syncErr == nil {
		interruptedSyncs = syncSummary.QueuedSyncs
	}

	totalInterrupted := interruptedJobs + interruptedSyncs

	if totalInterrupted > 0 && notifier.IsEnabled() {
		message := fmt.Sprintf("Grabarr is shutting down. %d job(s) and %d sync(s) have been queued for restart.",
			interruptedJobs, interruptedSyncs)
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

	// TODO: Add file logging support if logConfig.File is specified
	// For now, we only support stdout logging

	logger := slog.New(handler)
	slog.SetDefault(logger)
}