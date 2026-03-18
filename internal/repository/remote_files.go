package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"grabarr/internal/models"
)

// UpsertRemoteFile inserts or updates a remote file record.
// On conflict (same remote_path), updates name, size, watched_path, and last_seen_at.
func (r *Repository) UpsertRemoteFile(file *models.RemoteFile) error {
	query := `
		INSERT INTO remote_files (remote_path, name, size, extension, status, watched_path, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(remote_path) DO UPDATE SET
			name = excluded.name,
			size = excluded.size,
			watched_path = excluded.watched_path,
			last_seen_at = excluded.last_seen_at
		RETURNING id, first_seen_at, last_seen_at, updated_at
	`

	now := time.Now()
	if file.FirstSeenAt.IsZero() {
		file.FirstSeenAt = now
	}
	if file.LastSeenAt.IsZero() {
		file.LastSeenAt = now
	}

	err := r.db.QueryRow(query,
		file.RemotePath, file.Name, file.Size, file.Extension, string(file.Status),
		file.WatchedPath, file.FirstSeenAt, file.LastSeenAt,
	).Scan(&file.ID, &file.FirstSeenAt, &file.LastSeenAt, &file.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert remote file: %w", err)
	}

	return nil
}

// GetRemoteFiles returns a paginated list of remote files with optional filters.
func (r *Repository) GetRemoteFiles(filter models.RemoteFileFilter) ([]*models.RemoteFile, error) {
	conditions := []string{}
	args := []interface{}{}

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.WatchedPath != "" {
		conditions = append(conditions, "watched_path = ?")
		args = append(args, filter.WatchedPath)
	}
	if filter.Extension != "" {
		conditions = append(conditions, "extension = ?")
		args = append(args, filter.Extension)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files
		%s
		ORDER BY last_seen_at DESC
		LIMIT ? OFFSET ?
	`, where)

	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query remote files: %w", err)
	}
	defer rows.Close()

	var files []*models.RemoteFile
	for rows.Next() {
		f, err := scanRemoteFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, rows.Err()
}

// CountRemoteFiles returns the total count of remote files matching the filter.
func (r *Repository) CountRemoteFiles(filter models.RemoteFileFilter) (int, error) {
	conditions := []string{}
	args := []interface{}{}

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.WatchedPath != "" {
		conditions = append(conditions, "watched_path = ?")
		args = append(args, filter.WatchedPath)
	}
	if filter.Extension != "" {
		conditions = append(conditions, "extension = ?")
		args = append(args, filter.Extension)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`SELECT COUNT(*) FROM remote_files %s`, where)

	var count int
	if err := r.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count remote files: %w", err)
	}

	return count, nil
}

// GetRemoteFile returns a single remote file by ID.
func (r *Repository) GetRemoteFile(id int64) (*models.RemoteFile, error) {
	query := `
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files WHERE id = ?
	`

	rows, err := r.db.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote file: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("remote file %d not found", id)
	}

	return scanRemoteFile(rows)
}

// GetRemoteFileByPath returns a remote file by its remote path.
func (r *Repository) GetRemoteFileByPath(remotePath string) (*models.RemoteFile, error) {
	query := `
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files WHERE remote_path = ?
	`

	rows, err := r.db.Query(query, remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote file by path: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // not found is not an error here
	}

	return scanRemoteFile(rows)
}

// GetRemoteFileByJobID returns the remote file linked to a given job.
func (r *Repository) GetRemoteFileByJobID(jobID int64) (*models.RemoteFile, error) {
	query := `
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files WHERE job_id = ?
	`

	rows, err := r.db.Query(query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote file by job id: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	return scanRemoteFile(rows)
}

// UpdateRemoteFileStatus updates the status of a remote file.
func (r *Repository) UpdateRemoteFileStatus(id int64, status models.FileStatus) error {
	_, err := r.db.Exec("UPDATE remote_files SET status = ? WHERE id = ?", string(status), id)
	if err != nil {
		return fmt.Errorf("failed to update remote file status: %w", err)
	}
	return nil
}

// LinkRemoteFileToJob associates a remote file with a job and updates its status.
func (r *Repository) LinkRemoteFileToJob(remoteFileID, jobID int64, status models.FileStatus) error {
	_, err := r.db.Exec(
		"UPDATE remote_files SET job_id = ?, status = ? WHERE id = ?",
		jobID, string(status), remoteFileID,
	)
	if err != nil {
		return fmt.Errorf("failed to link remote file to job: %w", err)
	}
	return nil
}

// GetRemoteFilesLinkedToJobs returns all remote files that have a linked job_id.
func (r *Repository) GetRemoteFilesLinkedToJobs() ([]*models.RemoteFile, error) {
	query := `
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files
		WHERE job_id IS NOT NULL
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query linked remote files: %w", err)
	}
	defer rows.Close()

	var files []*models.RemoteFile
	for rows.Next() {
		f, err := scanRemoteFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, rows.Err()
}

// GetStaleRemoteFilesWithJobs returns remote files for a watched path that were not seen
// since seenBefore and have a linked job. Used to cancel jobs before stale cleanup.
func (r *Repository) GetStaleRemoteFilesWithJobs(watchedPath string, seenBefore time.Time) ([]*models.RemoteFile, error) {
	query := `
		SELECT id, remote_path, name, size, extension, status, job_id, watched_path,
		       first_seen_at, last_seen_at, updated_at
		FROM remote_files
		WHERE watched_path = ? AND last_seen_at < ? AND job_id IS NOT NULL
	`

	rows, err := r.db.Query(query, watchedPath, seenBefore)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale remote files with jobs: %w", err)
	}
	defer rows.Close()

	var files []*models.RemoteFile
	for rows.Next() {
		f, err := scanRemoteFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, rows.Err()
}

// DeleteStaleRemoteFiles removes remote files whose last_seen_at is before the given cutoff time.
// This is used to clean up files that were not seen during the most recent scan.
func (r *Repository) DeleteStaleRemoteFiles(watchedPath string, seenAfter time.Time) error {
	_, err := r.db.Exec(
		"DELETE FROM remote_files WHERE watched_path = ? AND last_seen_at < ?",
		watchedPath, seenAfter,
	)
	if err != nil {
		return fmt.Errorf("failed to delete stale remote files: %w", err)
	}
	return nil
}

func scanRemoteFile(rows *sql.Rows) (*models.RemoteFile, error) {
	var f models.RemoteFile
	var jobID sql.NullInt64
	var status string

	err := rows.Scan(
		&f.ID, &f.RemotePath, &f.Name, &f.Size, &f.Extension, &status, &jobID,
		&f.WatchedPath, &f.FirstSeenAt, &f.LastSeenAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan remote file: %w", err)
	}

	f.Status = models.FileStatus(status)
	if jobID.Valid {
		v := jobID.Int64
		f.JobID = &v
	}

	return &f, nil
}
