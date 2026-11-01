-- Per-job rclone settings. The rsync executor never read them.
ALTER TABLE jobs DROP COLUMN download_config;
