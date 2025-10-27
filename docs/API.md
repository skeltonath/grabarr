# API Reference

Complete API documentation for Grabarr's REST API.

## Base URL

```
http://your-server:8080/api/v1
```

## Response Format

All API responses follow this format:

```json
{
  "success": true,
  "data": { ... },
  "message": "optional message",
  "pagination": {
    "total": 100,
    "limit": 20,
    "offset": 0,
    "total_pages": 5,
    "page": 1
  }
}
```

## Job Management

### Create Job

**POST** `/jobs`

Create a new download job.

**Request Body:**

```json
{
  "name": "Movie.2024.1080p.mkv",
  "remote_path": "/home/user/torrents/Movie.2024.1080p/Movie.2024.1080p.mkv",
  "local_path": "movies/Movie.2024.1080p.mkv",
  "file_size": 2147483648,
  "priority": 5,
  "metadata": {
    "category": "movies",
    "torrent_name": "Movie.2024.1080p"
  },
  "download_config": {
    "bw_limit": "50M",
    "transfers": 4
  }
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Job display name |
| `remote_path` | string | Yes | Full path on seedbox |
| `local_path` | string | No | Custom local destination path |
| `file_size` | int64 | No | Size in bytes (enables gatekeeper checks) |
| `priority` | int | No | Job priority (higher = runs first, default: 5) |
| `metadata` | object | No | Custom metadata (category, torrent_name, etc.) |
| `download_config` | object | No | Per-job transfer settings |

**Download Config Options:**

| Field | Type | Description |
|-------|------|-------------|
| `bw_limit` | string | Overall bandwidth limit (e.g., "50M") |
| `bw_limit_file` | string | Per-file bandwidth limit |
| `transfers` | int | Number of parallel transfers |
| `checkers` | int | Number of simultaneous check operations |
| `multi_thread_streams` | int | Concurrent streams per file |

**Example:**

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Movie.2024.1080p.mkv",
    "remote_path": "/home/user/torrents/Movie.2024.1080p/Movie.2024.1080p.mkv",
    "file_size": 2147483648,
    "metadata": {
      "category": "movies"
    }
  }'
```

### Get Job

**GET** `/jobs/{id}`

Retrieve details for a specific job.

**Example:**

```bash
curl http://localhost:8080/api/v1/jobs/1
```

**Response:**

```json
{
  "success": true,
  "data": {
    "id": 1,
    "name": "Movie.2024.1080p.mkv",
    "remote_path": "/home/user/torrents/Movie.2024.1080p/Movie.2024.1080p.mkv",
    "local_path": "/unraid/user/media/downloads/movies/Movie.2024.1080p.mkv",
    "status": "completed",
    "priority": 5,
    "retries": 0,
    "max_retries": 5,
    "file_size": 2147483648,
    "progress": {
      "percentage": 100,
      "transferred_bytes": 2147483648,
      "total_bytes": 2147483648,
      "transfer_speed": 52428800,
      "eta": null
    },
    "metadata": {
      "category": "movies",
      "torrent_name": "Movie.2024.1080p"
    },
    "created_at": "2024-01-15T10:30:00Z",
    "started_at": "2024-01-15T10:30:05Z",
    "completed_at": "2024-01-15T10:35:20Z"
  }
}
```

### List Jobs

**GET** `/jobs`

List jobs with filtering, sorting, and pagination.

**Query Parameters:**

| Parameter | Type | Description | Default |
|-----------|------|-------------|---------|
| `status` | string | Filter by status (running, completed, failed, queued, pending, cancelled) | All statuses |
| `category` | string | Filter by metadata category | All categories |
| `torrent_name` | string | Filter by torrent name | All torrents |
| `limit` | int | Results per page | 50 |
| `offset` | int | Starting position | 0 |
| `sort_by` | string | Sort field (created_at, priority, progress, name) | created_at |
| `sort_order` | string | Sort direction (asc, desc) | desc |

**Example:**

```bash
curl "http://localhost:8080/api/v1/jobs?status=running&category=movies&limit=20&offset=0&sort_by=created_at&sort_order=desc"
```

**Response:**

```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "Movie.2024.1080p.mkv",
      "status": "running",
      "progress": {
        "percentage": 45.5,
        "transferred_bytes": 976894976,
        "total_bytes": 2147483648
      }
    }
  ],
  "pagination": {
    "total": 100,
    "limit": 20,
    "offset": 0,
    "total_pages": 5,
    "page": 1
  }
}
```

### Job Summary

**GET** `/jobs/summary`

Get aggregate statistics for all jobs.

**Example:**

```bash
curl http://localhost:8080/api/v1/jobs/summary
```

**Response:**

```json
{
  "success": true,
  "data": {
    "total_jobs": 150,
    "queued_jobs": 5,
    "running_jobs": 3,
    "completed_jobs": 135,
    "failed_jobs": 7,
    "cancelled_jobs": 0
  }
}
```

### Retry Job

**POST** `/jobs/{id}/retry`

Retry a failed job. Resets the job to queued status with full retry attempts.

**Example:**

```bash
curl -X POST http://localhost:8080/api/v1/jobs/1/retry
```

**Response:**

```json
{
  "success": true,
  "message": "Job queued for retry"
}
```

### Cancel Job

**POST** `/jobs/{id}/cancel`

Cancel a running or queued job.

**Example:**

```bash
curl -X POST http://localhost:8080/api/v1/jobs/1/cancel
```

**Response:**

```json
{
  "success": true,
  "message": "Job cancelled successfully"
}
```

### Delete Job

**DELETE** `/jobs/{id}`

Permanently delete a job from the database.

**Example:**

```bash
curl -X DELETE http://localhost:8080/api/v1/jobs/1
```

**Response:**

```json
{
  "success": true,
  "message": "Job deleted successfully"
}
```

## System Monitoring

### Health Check

**GET** `/health`

Check service health and readiness.

**Example:**

```bash
curl http://localhost:8080/api/v1/health
```

**Response:**

```json
{
  "success": true,
  "data": {
    "status": "healthy",
    "timestamp": "2024-01-15T10:30:00Z"
  }
}
```

### System Status

**GET** `/status`

Get detailed system status including gatekeeper resource information.

**Example:**

```bash
curl http://localhost:8080/api/v1/status
```

**Response:**

```json
{
  "success": true,
  "data": {
    "service": "grabarr",
    "version": "1.0.0",
    "uptime": "2h15m30s",
    "gatekeeper": {
      "bandwidth_usage_mbps": 245.5,
      "bandwidth_limit_mbps": 500,
      "cache_usage_percent": 65.2,
      "cache_max_percent": 80,
      "cache_free_bytes": 107374182400,
      "cache_total_bytes": 322122547200
    },
    "jobs": {
      "active": 3,
      "queued": 5,
      "max_concurrent": 5
    }
  }
}
```

### Metrics

**GET** `/metrics`

Get comprehensive system metrics.

**Example:**

```bash
curl http://localhost:8080/api/v1/metrics
```

**Response:**

```json
{
  "success": true,
  "data": {
    "bandwidth": {
      "current_mbps": 245.5,
      "limit_mbps": 500,
      "utilization_percent": 49.1
    },
    "disk": {
      "cache_used_bytes": 214748364800,
      "cache_total_bytes": 322122547200,
      "cache_free_bytes": 107374182400,
      "cache_usage_percent": 65.2
    },
    "jobs": {
      "total": 150,
      "running": 3,
      "queued": 5,
      "completed": 135,
      "failed": 7
    },
    "transfers": {
      "active_count": 3,
      "total_bytes_transferred": 536870912000,
      "average_speed_mbps": 82.5
    }
  }
}
```

## Error Responses

All errors follow this format:

```json
{
  "success": false,
  "error": "Error message describing what went wrong"
}
```

### Common HTTP Status Codes

| Code | Description |
|------|-------------|
| 200 | Success |
| 201 | Created (new job) |
| 400 | Bad Request (invalid input) |
| 404 | Not Found (job doesn't exist) |
| 500 | Internal Server Error |

## Rate Limiting

Currently, there is no rate limiting on the API. Use responsibly.

## Authentication

Currently, there is no authentication required. Consider using a reverse proxy (nginx, Caddy) with authentication if exposing to the internet, or use Cloudflare Access as shown in the qBittorrent integration.
