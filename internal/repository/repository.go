package repository

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"grabarr/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaFS embed.FS

type Repository struct {
	db *sql.DB
}

func New(dbPath string) (*Repository, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_timeout=5000&_cache_size=2000", dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	repo := &Repository{db: db}

	if err := repo.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return repo, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) initSchema() error {
	schemaSQL, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	_, err = r.db.Exec(string(schemaSQL))
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	// Run migrations for existing databases
	if err := r.runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// runMigrations applies database migrations for schema changes
func (r *Repository) runMigrations() error {
	// Migration 1: Add download_config column to jobs table
	var hasJobsDownloadConfig bool
	row := r.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='download_config'")
	if err := row.Scan(&hasJobsDownloadConfig); err != nil {
		return fmt.Errorf("failed to check for download_config column in jobs: %w", err)
	}

	if !hasJobsDownloadConfig {
		slog.Info("migrating database: adding download_config column to jobs table")
		_, err := r.db.Exec("ALTER TABLE jobs ADD COLUMN download_config TEXT")
		if err != nil {
			return fmt.Errorf("failed to add download_config column to jobs: %w", err)
		}
		slog.Info("migration complete: download_config column added to jobs table")
	}

	// Migration 2: Add download_config column to sync_jobs table
	var hasSyncJobsDownloadConfig bool
	row = r.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('sync_jobs') WHERE name='download_config'")
	if err := row.Scan(&hasSyncJobsDownloadConfig); err != nil {
		return fmt.Errorf("failed to check for download_config column in sync_jobs: %w", err)
	}

	if !hasSyncJobsDownloadConfig {
		slog.Info("migrating database: adding download_config column to sync_jobs table")
		_, err := r.db.Exec("ALTER TABLE sync_jobs ADD COLUMN download_config TEXT")
		if err != nil {
			return fmt.Errorf("failed to add download_config column to sync_jobs: %w", err)
		}
		slog.Info("migration complete: download_config column added to sync_jobs table")
	}

	return nil
}

// Job operations
func (r *Repository) CreateJob(job *models.Job) error {
	query := `
		INSERT INTO jobs (
			name, remote_path, local_path, status, priority, max_retries,
			progress, metadata, download_config, estimated_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query,
		job.Name, job.RemotePath, job.LocalPath, job.Status, job.Priority,
		job.MaxRetries, job.Progress, job.Metadata, job.DownloadConfig, job.EstimatedSize)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get job ID: %w", err)
	}

	job.ID = id
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()

	return nil
}

func (r *Repository) GetJob(id int64) (*models.Job, error) {
	query := `
		SELECT id, name, remote_path, local_path, status, priority, retries, max_retries,
			   error_message, progress, metadata, download_config, created_at, updated_at, started_at,
			   completed_at, estimated_size, transferred_bytes, transfer_speed
		FROM jobs WHERE id = ?
	`

	var job models.Job
	var errorMessage sql.NullString
	var startedAt, completedAt sql.NullTime
	var downloadConfig sql.NullString

	err := r.db.QueryRow(query, id).Scan(
		&job.ID, &job.Name, &job.RemotePath, &job.LocalPath, &job.Status,
		&job.Priority, &job.Retries, &job.MaxRetries, &errorMessage,
		&job.Progress, &job.Metadata, &downloadConfig, &job.CreatedAt, &job.UpdatedAt,
		&startedAt, &completedAt, &job.EstimatedSize, &job.TransferredBytes,
		&job.TransferSpeed)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job %d not found", id)
		}
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	if errorMessage.Valid {
		job.ErrorMessage = errorMessage.String
	}
	if downloadConfig.Valid && downloadConfig.String != "" {
		// Download config is stored as JSON, use the Scan method
		job.DownloadConfig = &models.DownloadConfig{}
		if err := job.DownloadConfig.Scan(downloadConfig.String); err != nil {
			slog.Warn("failed to parse download_config, ignoring", "job_id", id, "error", err)
			job.DownloadConfig = nil
		}
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

func (r *Repository) GetJobs(filter models.JobFilter) ([]*models.Job, error) {
	query := `
		SELECT id, name, remote_path, local_path, status, priority, retries, max_retries,
			   error_message, progress, metadata, download_config, created_at, updated_at, started_at,
			   completed_at, estimated_size, transferred_bytes, transfer_speed
		FROM jobs
	`

	var conditions []string
	var args []interface{}

	if len(filter.Status) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Status))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", placeholders))
		for _, status := range filter.Status {
			args = append(args, status)
		}
	}

	if filter.Category != "" {
		conditions = append(conditions, "JSON_EXTRACT(metadata, '$.category') = ?")
		args = append(args, filter.Category)
	}

	if filter.MinPriority != nil {
		conditions = append(conditions, "priority >= ?")
		args = append(args, *filter.MinPriority)
	}

	if filter.MaxPriority != nil {
		conditions = append(conditions, "priority <= ?")
		args = append(args, *filter.MaxPriority)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Sorting
	sortBy := "created_at"
	if filter.SortBy != "" {
		sortBy = filter.SortBy
	}
	sortOrder := "DESC"
	if filter.SortOrder != "" {
		sortOrder = filter.SortOrder
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Pagination
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var errorMessage sql.NullString
		var startedAt, completedAt sql.NullTime
		var downloadConfig sql.NullString

		err := rows.Scan(
			&job.ID, &job.Name, &job.RemotePath, &job.LocalPath, &job.Status,
			&job.Priority, &job.Retries, &job.MaxRetries, &errorMessage,
			&job.Progress, &job.Metadata, &downloadConfig, &job.CreatedAt, &job.UpdatedAt,
			&startedAt, &completedAt, &job.EstimatedSize, &job.TransferredBytes,
			&job.TransferSpeed)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		if errorMessage.Valid {
			job.ErrorMessage = errorMessage.String
		}
		if downloadConfig.Valid && downloadConfig.String != "" {
			job.DownloadConfig = &models.DownloadConfig{}
			if err := job.DownloadConfig.Scan(downloadConfig.String); err != nil {
				slog.Warn("failed to parse download_config, ignoring", "job_id", job.ID, "error", err)
				job.DownloadConfig = nil
			}
		}
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jobs: %w", err)
	}

	return jobs, nil
}

func (r *Repository) UpdateJob(job *models.Job) error {
	query := `
		UPDATE jobs SET
			status = ?, priority = ?, retries = ?, error_message = ?,
			progress = ?, started_at = ?, completed_at = ?,
			transferred_bytes = ?, transfer_speed = ?
		WHERE id = ?
	`

	_, err := r.db.Exec(query,
		job.Status, job.Priority, job.Retries, job.ErrorMessage,
		job.Progress, job.StartedAt, job.CompletedAt,
		job.TransferredBytes, job.TransferSpeed, job.ID)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

func (r *Repository) DeleteJob(id int64) error {
	_, err := r.db.Exec("DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

func (r *Repository) GetJobSummary() (*models.JobSummary, error) {
	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END) as queued,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) as running,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
			SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancelled
		FROM jobs
	`

	var summary models.JobSummary
	err := r.db.QueryRow(query).Scan(
		&summary.TotalJobs, &summary.QueuedJobs, &summary.PendingJobs,
		&summary.RunningJobs, &summary.CompletedJobs, &summary.FailedJobs,
		&summary.CancelledJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to get job summary: %w", err)
	}

	return &summary, nil
}

// Job attempt operations
func (r *Repository) CreateJobAttempt(attempt *models.JobAttempt) error {
	query := `
		INSERT INTO job_attempts (job_id, attempt_num, status, error_message, log_data)
		VALUES (?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query, attempt.JobID, attempt.AttemptNum,
		attempt.Status, attempt.ErrorMessage, attempt.LogData)
	if err != nil {
		return fmt.Errorf("failed to create job attempt: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get attempt ID: %w", err)
	}

	attempt.ID = id
	attempt.StartedAt = time.Now()

	return nil
}

func (r *Repository) UpdateJobAttempt(attempt *models.JobAttempt) error {
	query := `
		UPDATE job_attempts SET
			status = ?, error_message = ?, ended_at = ?, log_data = ?
		WHERE id = ?
	`

	_, err := r.db.Exec(query, attempt.Status, attempt.ErrorMessage,
		attempt.EndedAt, attempt.LogData, attempt.ID)
	if err != nil {
		return fmt.Errorf("failed to update job attempt: %w", err)
	}

	return nil
}

func (r *Repository) GetJobAttempts(jobID int64) ([]*models.JobAttempt, error) {
	query := `
		SELECT id, job_id, attempt_num, status, error_message, started_at, ended_at, log_data
		FROM job_attempts
		WHERE job_id = ?
		ORDER BY attempt_num DESC
	`

	rows, err := r.db.Query(query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job attempts: %w", err)
	}
	defer rows.Close()

	var attempts []*models.JobAttempt
	for rows.Next() {
		var attempt models.JobAttempt
		var errorMessage sql.NullString
		var endedAt sql.NullTime
		var logData sql.NullString

		err := rows.Scan(&attempt.ID, &attempt.JobID, &attempt.AttemptNum,
			&attempt.Status, &errorMessage, &attempt.StartedAt, &endedAt, &logData)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job attempt: %w", err)
		}

		if errorMessage.Valid {
			attempt.ErrorMessage = errorMessage.String
		}
		if endedAt.Valid {
			attempt.EndedAt = &endedAt.Time
		}
		if logData.Valid {
			attempt.LogData = logData.String
		}

		attempts = append(attempts, &attempt)
	}

	return attempts, nil
}

// System configuration operations
func (r *Repository) GetConfig(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("config key %s not found", key)
		}
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	return value, nil
}

func (r *Repository) SetConfig(key, value string) error {
	query := `
		INSERT INTO system_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query, key, value, value)
	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}

	return nil
}

// Sync job operations
func (r *Repository) CreateSyncJob(syncJob *models.SyncJob) error {
	query := `
		INSERT INTO sync_jobs (
			remote_path, local_path, status, progress, stats, download_config
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query,
		syncJob.RemotePath, syncJob.LocalPath, syncJob.Status,
		syncJob.Progress, syncJob.Stats, syncJob.DownloadConfig)
	if err != nil {
		return fmt.Errorf("failed to create sync job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get sync job ID: %w", err)
	}

	syncJob.ID = id
	syncJob.CreatedAt = time.Now()
	syncJob.UpdatedAt = time.Now()

	return nil
}

func (r *Repository) GetSyncJob(id int64) (*models.SyncJob, error) {
	query := `
		SELECT id, remote_path, local_path, status, error_message,
			   progress, stats, download_config, created_at, updated_at, started_at,
			   completed_at, rclone_job_id
		FROM sync_jobs WHERE id = ?
	`

	var syncJob models.SyncJob
	var errorMessage sql.NullString
	var startedAt, completedAt sql.NullTime
	var rcloneJobID sql.NullInt64
	var downloadConfig sql.NullString

	err := r.db.QueryRow(query, id).Scan(
		&syncJob.ID, &syncJob.RemotePath, &syncJob.LocalPath, &syncJob.Status,
		&errorMessage, &syncJob.Progress, &syncJob.Stats, &downloadConfig, &syncJob.CreatedAt,
		&syncJob.UpdatedAt, &startedAt, &completedAt, &rcloneJobID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("sync job %d not found", id)
		}
		return nil, fmt.Errorf("failed to get sync job: %w", err)
	}

	if errorMessage.Valid {
		syncJob.ErrorMessage = errorMessage.String
	}
	if downloadConfig.Valid && downloadConfig.String != "" {
		syncJob.DownloadConfig = &models.DownloadConfig{}
		if err := syncJob.DownloadConfig.Scan(downloadConfig.String); err != nil {
			slog.Warn("failed to parse download_config, ignoring", "sync_id", id, "error", err)
			syncJob.DownloadConfig = nil
		}
	}
	if startedAt.Valid {
		syncJob.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		syncJob.CompletedAt = &completedAt.Time
	}
	if rcloneJobID.Valid {
		syncJob.RCloneJobID = &rcloneJobID.Int64
	}

	return &syncJob, nil
}

func (r *Repository) GetSyncJobs(filter models.SyncFilter) ([]*models.SyncJob, error) {
	query := `
		SELECT id, remote_path, local_path, status, error_message,
			   progress, stats, download_config, created_at, updated_at, started_at,
			   completed_at, rclone_job_id
		FROM sync_jobs
	`

	var conditions []string
	var args []interface{}

	if len(filter.Status) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Status))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", placeholders))
		for _, status := range filter.Status {
			args = append(args, status)
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Sorting
	sortBy := "created_at"
	if filter.SortBy != "" {
		sortBy = filter.SortBy
	}
	sortOrder := "DESC"
	if filter.SortOrder != "" {
		sortOrder = filter.SortOrder
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Pagination
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query sync jobs: %w", err)
	}
	defer rows.Close()

	var syncJobs []*models.SyncJob
	for rows.Next() {
		var syncJob models.SyncJob
		var errorMessage sql.NullString
		var startedAt, completedAt sql.NullTime
		var rcloneJobID sql.NullInt64
		var downloadConfig sql.NullString

		err := rows.Scan(
			&syncJob.ID, &syncJob.RemotePath, &syncJob.LocalPath, &syncJob.Status,
			&errorMessage, &syncJob.Progress, &syncJob.Stats, &downloadConfig, &syncJob.CreatedAt,
			&syncJob.UpdatedAt, &startedAt, &completedAt, &rcloneJobID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan sync job: %w", err)
		}

		if errorMessage.Valid {
			syncJob.ErrorMessage = errorMessage.String
		}
		if downloadConfig.Valid && downloadConfig.String != "" {
			syncJob.DownloadConfig = &models.DownloadConfig{}
			if err := syncJob.DownloadConfig.Scan(downloadConfig.String); err != nil {
				slog.Warn("failed to parse download_config, ignoring", "sync_id", syncJob.ID, "error", err)
				syncJob.DownloadConfig = nil
			}
		}
		if startedAt.Valid {
			syncJob.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			syncJob.CompletedAt = &completedAt.Time
		}
		if rcloneJobID.Valid {
			syncJob.RCloneJobID = &rcloneJobID.Int64
		}

		syncJobs = append(syncJobs, &syncJob)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sync jobs: %w", err)
	}

	return syncJobs, nil
}

func (r *Repository) UpdateSyncJob(syncJob *models.SyncJob) error {
	query := `
		UPDATE sync_jobs SET
			status = ?, error_message = ?, progress = ?, stats = ?,
			started_at = ?, completed_at = ?, rclone_job_id = ?
		WHERE id = ?
	`

	_, err := r.db.Exec(query,
		syncJob.Status, syncJob.ErrorMessage, syncJob.Progress, syncJob.Stats,
		syncJob.StartedAt, syncJob.CompletedAt, syncJob.RCloneJobID, syncJob.ID)
	if err != nil {
		return fmt.Errorf("failed to update sync job: %w", err)
	}

	return nil
}

func (r *Repository) DeleteSyncJob(id int64) error {
	_, err := r.db.Exec("DELETE FROM sync_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete sync job: %w", err)
	}

	return nil
}

func (r *Repository) GetSyncSummary() (*models.SyncSummary, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END), 0) as queued,
			COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0) as running,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0) as cancelled
		FROM sync_jobs
	`

	var summary models.SyncSummary
	err := r.db.QueryRow(query).Scan(
		&summary.TotalSyncs, &summary.QueuedSyncs, &summary.RunningSyncs,
		&summary.CompletedSyncs, &summary.FailedSyncs, &summary.CancelledSyncs)
	if err != nil {
		return nil, fmt.Errorf("failed to get sync summary: %w", err)
	}

	return &summary, nil
}

func (r *Repository) GetActiveSyncJobsCount() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM sync_jobs WHERE status IN ('queued', 'running')").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get active sync jobs count: %w", err)
	}
	return count, nil
}

// Cleanup operations
func (r *Repository) CleanupOldJobs(completedBefore, failedBefore time.Time) (int, error) {
	query := `
		DELETE FROM jobs
		WHERE (status = 'completed' AND completed_at < ?)
		   OR (status = 'failed' AND updated_at < ?)
	`

	result, err := r.db.Exec(query, completedBefore, failedBefore)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old jobs: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get affected rows: %w", err)
	}

	slog.Info("cleaned up old jobs", "count", rowsAffected)
	return int(rowsAffected), nil
}
