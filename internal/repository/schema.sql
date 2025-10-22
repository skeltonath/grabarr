-- Migration note for existing databases:
-- If upgrading from a version without download_config, run:
-- ALTER TABLE jobs ADD COLUMN download_config TEXT;

-- Jobs table
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    remote_path TEXT NOT NULL,
    local_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 0,
    retries INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    error_message TEXT,
    progress TEXT, -- JSON blob
    metadata TEXT, -- JSON blob
    download_config TEXT, -- JSON blob
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    completed_at DATETIME,
    estimated_size INTEGER DEFAULT 0,
    transferred_bytes INTEGER DEFAULT 0,
    transfer_speed INTEGER DEFAULT 0
);

-- Job attempts table for tracking retry history
CREATE TABLE IF NOT EXISTS job_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL,
    attempt_num INTEGER NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    log_data TEXT,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

-- System configuration table for runtime settings
CREATE TABLE IF NOT EXISTS system_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority);
CREATE INDEX IF NOT EXISTS idx_jobs_status_priority ON jobs(status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_job_attempts_job_id ON job_attempts(job_id);
CREATE INDEX IF NOT EXISTS idx_job_attempts_attempt_num ON job_attempts(job_id, attempt_num);

-- Triggers to automatically update updated_at timestamp
CREATE TRIGGER IF NOT EXISTS jobs_updated_at
    AFTER UPDATE ON jobs
    FOR EACH ROW
BEGIN
    UPDATE jobs SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS system_config_updated_at
    AFTER UPDATE ON system_config
    FOR EACH ROW
BEGIN
    UPDATE system_config SET updated_at = CURRENT_TIMESTAMP WHERE key = NEW.key;
END;

-- Initial system configuration values
INSERT OR IGNORE INTO system_config (key, value, description) VALUES
    ('schema_version', '1', 'Database schema version'),
    ('last_cleanup', '1970-01-01T00:00:00Z', 'Last time job cleanup was performed'),
    ('last_backup', '1970-01-01T00:00:00Z', 'Last time database backup was performed'),
    ('service_start_time', '1970-01-01T00:00:00Z', 'Last service start time'),
    ('total_downloads', '0', 'Total number of completed downloads'),
    ('total_bytes_downloaded', '0', 'Total bytes downloaded since service start');