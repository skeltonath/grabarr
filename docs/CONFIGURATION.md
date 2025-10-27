# Configuration Reference

Complete configuration guide for Grabarr.

## Configuration Files

Grabarr uses three configuration files:

1. **config.yaml** - Main application configuration
2. **.env** - Environment variables
3. **rclone.conf** - Rclone remote configuration (legacy, not actively used with rsync)

## Configuration Loading

The service looks for configuration in this order:

1. `$GRABARR_CONFIG` environment variable
2. `/config/config.yaml`
3. `./config.yaml`

## config.yaml

### Complete Example

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  shutdown_timeout: "30s"

downloads:
  local_path: "/unraid/user/media/downloads/"
  allowed_categories: ["movies", "tv", "anime"]

rsync:
  ssh_host: "your-seedbox.example.com"
  ssh_user: "your-username"
  ssh_key_file: "/config/grabarr_rsa"

gatekeeper:
  seedbox:
    bandwidth_limit_mbps: 500
    check_interval: "30s"
  cache_disk:
    path: "/unraid/cache"
    max_usage_percent: 80
    check_interval: "30s"
  rules:
    require_filesize_check: true

jobs:
  max_concurrent: 5
  max_retries: 5
  cleanup_completed_after: "168h"
  cleanup_failed_after: "720h"

database:
  path: "/data/grabarr.db"

notifications:
  pushover:
    token: "${PUSHOVER_TOKEN}"
    user: "${PUSHOVER_USER}"
    enabled: true
    priority: 0
    retry_interval: "60s"
    expire_time: "3600s"

logging:
  level: "info"
  format: "json"
```

## Configuration Sections

### Server

HTTP server configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `server.port` | int | Yes | HTTP server port | 8080 |
| `server.host` | string | Yes | Bind address (0.0.0.0 for all interfaces) | "0.0.0.0" |
| `server.shutdown_timeout` | duration | Yes | Graceful shutdown timeout | "30s" |

**Example:**

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  shutdown_timeout: "30s"
```

### Downloads

Local download configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `downloads.local_path` | string | Yes | Local download directory | None |
| `downloads.allowed_categories` | []string | No | Whitelist of allowed categories (empty = all allowed) | [] |

**Example:**

```yaml
downloads:
  local_path: "/unraid/user/media/downloads/"
  allowed_categories: ["movies", "tv", "anime"]  # Optional
```

**Notes:**
- `local_path` should be the base directory where files are downloaded
- If `allowed_categories` is set, jobs with categories not in this list will be rejected
- Leave `allowed_categories` empty or omit it to allow all categories

### Rsync

SSH connection configuration for rsync transfers.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `rsync.ssh_host` | string | Yes | Seedbox hostname or IP | None |
| `rsync.ssh_user` | string | Yes | SSH username | None |
| `rsync.ssh_key_file` | string | Yes | Path to SSH private key | None |

**Example:**

```yaml
rsync:
  ssh_host: "pandora.whatbox.ca"
  ssh_user: "psychomanteum"
  ssh_key_file: "/config/grabarr_rsa"
```

**Notes:**
- SSH key must be passwordless for automation
- Key file must be readable by the container user (99:100 on Unraid)
- Public key must be added to seedbox's `~/.ssh/authorized_keys`

### Gatekeeper

Resource monitoring and admission control.

#### Seedbox Bandwidth

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `gatekeeper.seedbox.bandwidth_limit_mbps` | int | Yes | Maximum bandwidth in Mbps | None |
| `gatekeeper.seedbox.check_interval` | duration | Yes | How often to check bandwidth usage | "30s" |

**Example:**

```yaml
gatekeeper:
  seedbox:
    bandwidth_limit_mbps: 500  # 500Mbps out of 1Gbps connection
    check_interval: "30s"
```

#### Cache Disk

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `gatekeeper.cache_disk.path` | string | Yes | Path to cache disk to monitor | None |
| `gatekeeper.cache_disk.max_usage_percent` | int | Yes | Maximum cache usage percentage | 80 |
| `gatekeeper.cache_disk.check_interval` | duration | Yes | How often to check disk usage | "30s" |

**Example:**

```yaml
gatekeeper:
  cache_disk:
    path: "/unraid/cache"
    max_usage_percent: 80
    check_interval: "30s"
```

#### Rules

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `gatekeeper.rules.require_filesize_check` | bool | Yes | Verify file will fit before starting | true |

**Example:**

```yaml
gatekeeper:
  rules:
    require_filesize_check: true
```

**Gatekeeper Behavior:**
- Jobs are queued when resources are constrained
- Checks run every 5 seconds to start queued jobs when resources become available
- If checks fail, logs errors but continues operation

### Jobs

Job queue and lifecycle configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `jobs.max_concurrent` | int | Yes | Maximum concurrent downloads | 5 |
| `jobs.max_retries` | int | Yes | Maximum retry attempts per job | 5 |
| `jobs.cleanup_completed_after` | duration | Yes | Delete completed jobs after this duration | "168h" (7 days) |
| `jobs.cleanup_failed_after` | duration | Yes | Delete failed jobs after this duration | "720h" (30 days) |

**Example:**

```yaml
jobs:
  max_concurrent: 5
  max_retries: 5
  cleanup_completed_after: "168h"  # 7 days
  cleanup_failed_after: "720h"     # 30 days
```

**Notes:**
- `max_concurrent` controls how many jobs can download simultaneously
- Jobs are automatically retried up to `max_retries` times
- Manual retry via API resets the retry counter
- Cleanup runs hourly

### Database

SQLite database configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `database.path` | string | Yes | Path to SQLite database file | "/data/grabarr.db" |

**Example:**

```yaml
database:
  path: "/data/grabarr.db"
```

**Notes:**
- Directory must exist and be writable
- Database is created automatically if it doesn't exist

### Notifications

Pushover notification configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `notifications.pushover.enabled` | bool | Yes | Enable Pushover notifications | false |
| `notifications.pushover.token` | string | Conditional | Pushover app token (required if enabled) | "" |
| `notifications.pushover.user` | string | Conditional | Pushover user key (required if enabled) | "" |
| `notifications.pushover.priority` | int | Yes | Message priority (-2 to 2) | 0 |
| `notifications.pushover.retry_interval` | duration | Yes | Retry interval for priority 2 messages | "60s" |
| `notifications.pushover.expire_time` | duration | Yes | Expiration time for priority 2 messages | "3600s" |

**Example:**

```yaml
notifications:
  pushover:
    token: "${PUSHOVER_TOKEN}"
    user: "${PUSHOVER_USER}"
    enabled: true
    priority: 0
    retry_interval: "60s"
    expire_time: "3600s"
```

**Priority Levels:**
- `-2`: No notification/alert
- `-1`: Always send as a quiet notification
- `0`: Normal priority
- `1`: High priority (bypasses quiet hours)
- `2`: Emergency priority (requires acknowledgment)

**Notes:**
- Use environment variable expansion for credentials: `"${PUSHOVER_TOKEN}"`
- Notifications are sent for job failures and system alerts
- Completed jobs only notify if job priority >= 5

### Logging

Application logging configuration.

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| `logging.level` | string | Yes | Log level (debug, info, warn, error) | "info" |
| `logging.format` | string | Yes | Log format (json, text) | "json" |

**Example:**

```yaml
logging:
  level: "info"
  format: "json"
```

**Log Levels:**
- `debug`: Detailed debugging information
- `info`: General informational messages
- `warn`: Warning messages
- `error`: Error messages only

**Log Formats:**
- `json`: Structured JSON logs (recommended for production)
- `text`: Human-readable text logs (easier for local development)

## Environment Variables

### .env File

```bash
# Pushover Notifications (optional)
PUSHOVER_TOKEN=your_pushover_app_token
PUSHOVER_USER=your_pushover_user_key

# Timezone (optional, defaults to system timezone)
TZ=America/Los_Angeles

# Config file override (optional)
GRABARR_CONFIG=/path/to/config.yaml
```

### Environment Variable Expansion

Environment variables can be used in config.yaml using `${VAR_NAME}` syntax:

```yaml
notifications:
  pushover:
    token: "${PUSHOVER_TOKEN}"  # Expands to value from environment
    user: "${PUSHOVER_USER}"
```

## Duration Format

Durations use Go's duration format:

- `s` - seconds
- `m` - minutes
- `h` - hours

**Examples:**
- `30s` - 30 seconds
- `5m` - 5 minutes
- `2h30m` - 2 hours 30 minutes
- `168h` - 7 days (168 hours)

## Configuration Hot-Reload

Grabarr watches for configuration file changes and automatically reloads when `config.yaml` is modified:

- Server, jobs, logging, and notification settings are reloaded
- No service restart required
- Changes take effect immediately
- Invalid configuration changes are rejected and logged

**Not hot-reloadable:**
- Database path
- Downloads path (requires restart)
- Rsync configuration (requires restart)

## Validation

Configuration is validated on startup and reload:

- Port must be between 1 and 65535
- `max_concurrent` must be greater than 0
- `max_retries` cannot be negative
- Pushover credentials required if notifications enabled
- Required paths must exist or be creatable

Invalid configuration will prevent startup and log error details.

## Security Best Practices

1. **Environment Variables**: Store sensitive data (tokens, keys) in `.env`
2. **File Permissions**:
   - config.yaml: 644
   - .env: 600
   - SSH keys: 600
3. **Reverse Proxy**: Use nginx/Caddy with authentication for internet exposure
4. **Cloudflare Access**: Use CF Access for additional security layer
5. **Network Isolation**: Run in Docker network, not host mode

## Troubleshooting

### Configuration Not Found

Check in order:
1. `$GRABARR_CONFIG` environment variable
2. `/config/config.yaml`
3. `./config.yaml`

### Environment Variables Not Expanding

Ensure format is: `"${VAR_NAME}"` (with quotes)

### Pushover Validation Error

Check that:
- Token and user are not empty
- Values don't start with `${` (not expanded)
- `.env` file is loaded in docker-compose

### Hot-Reload Not Working

- Check file permissions (must be readable)
- Check logs for validation errors
- Some settings require restart (database, downloads path)
