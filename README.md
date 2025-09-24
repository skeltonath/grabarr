# Grabarr

A Go-based service for intelligently managing downloads from a remote seedbox using rclone. Designed to run in Docker containers on Unraid servers.

## Features

- **Intelligent Job Scheduling**: Monitors seedbox bandwidth and local disk space to optimize download timing
- **REST API**: Complete API for job management and monitoring
- **Retry Logic**: Automatic retry with exponential backoff for failed downloads
- **Progress Tracking**: Real-time download progress with ETA calculations
- **Pushover Notifications**: Alerts for job failures and completions
- **Configuration Hot-Reload**: Update settings without container restart
- **Persistent Storage**: SQLite database with job history and state
- **Resource Monitoring**: Bandwidth and disk space monitoring
- **Docker Ready**: Container-optimized with health checks

## Quick Start

### 1. Configuration

Copy the example configuration and customize it:

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml` with your settings:

```yaml
rclone:
  remote_name: "your-seedbox"
  remote_path: "downloads/completed/"
  config_file: "/config/rclone.conf"

notifications:
  pushover:
    token: "your-pushover-app-token"
    user: "your-pushover-user-key"
    enabled: true
```

### 2. RClone Configuration

Set up your rclone configuration file:

```bash
rclone config
# Configure your seedbox remote
```

### 3. Docker Compose

```yaml
version: '3.8'
services:
  grabarr:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/config/config.yaml:ro
      - ./rclone.conf:/config/rclone.conf:ro
      - ./data:/data
      - /mnt/user:/data:rw  # Unraid array
    environment:
      - PUSHOVER_TOKEN=your_token
      - PUSHOVER_USER=your_user
```

### 4. Start the Service

```bash
docker-compose up -d
```

## API Usage

### Queue a Download Job

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Movie.2023.1080p",
    "remote_path": "Movie.2023.1080p",
    "priority": 5,
    "metadata": {
      "category": "movies",
      "qbittorrent_hash": "abc123..."
    }
  }'
```

### Get Job Status

```bash
curl http://localhost:8080/api/v1/jobs/1
```

### List All Jobs

```bash
curl "http://localhost:8080/api/v1/jobs?status=running&limit=10"
```

### System Health

```bash
curl http://localhost:8080/api/v1/health
```

## QBittorrent Integration

Configure qBittorrent to call Grabarr when downloads complete:

1. Go to Tools → Options → Downloads
2. Enable "Run external program on torrent completion"
3. Set command: `curl -X POST http://grabarr:8080/api/v1/jobs -H "Content-Type: application/json" -d '{"name":"%N","remote_path":"%N","metadata":{"qbittorrent_hash":"%I","category":"%L"}}'`

## Configuration Reference

### Server Settings
- `server.port`: HTTP server port (default: 8080)
- `server.host`: Bind address (default: "0.0.0.0")

### RClone Settings
- `rclone.remote_name`: RClone remote name
- `rclone.remote_path`: Base path on remote
- `rclone.local_path`: Local download directory
- `rclone.bandwidth_limit`: Download bandwidth limit (e.g., "50M")

### Resource Management
- `resources.bandwidth.max_usage_percent`: Max bandwidth usage percentage
- `resources.disk.cache_drive_min_free`: Minimum free space on cache drive
- `resources.disk.array_min_free`: Minimum free space on array

### Job Management
- `jobs.max_concurrent`: Maximum concurrent downloads
- `jobs.max_retries`: Maximum retry attempts
- `jobs.retry_backoff_base`: Base retry delay (exponential backoff)

## Monitoring

### Metrics Endpoint

```bash
curl http://localhost:8080/api/v1/metrics
```

Returns:
- Bandwidth usage statistics
- Disk space information
- Job queue statistics
- System performance metrics

### Health Check

```bash
curl http://localhost:8080/api/v1/health
```

Returns service health status with resource availability.

## Development

### Building

```bash
go mod download
go build -o grabarr ./cmd/grabarr
```

### Running Locally

```bash
./grabarr
```

The service will look for configuration in:
1. `$GRABARR_CONFIG` environment variable
2. `/config/config.yaml`
3. `./config.yaml`
4. `./config.example.yaml`

### Testing

```bash
go test ./...
```

## Architecture

- **API Layer**: HTTP REST API with Gorilla Mux
- **Queue System**: In-memory job queue with persistent SQLite storage
- **Executor**: RClone process management with progress tracking
- **Monitor**: Resource monitoring (bandwidth, disk space)
- **Notifications**: Pushover integration for alerts
- **Configuration**: YAML-based config with hot-reload

## License

MIT License - see LICENSE file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request