# Grabarr

A Go-based download orchestration service for managing file transfers from remote seedboxes. Features a modern web UI, intelligent resource management, and automatic retry logic. Designed for Docker deployment on Unraid servers.

## Features

- **Web UI Dashboard** - Modern interface with dark mode, real-time updates, and job grouping
- **Intelligent Resource Management** - Gatekeeper system monitors bandwidth and disk space
- **Automatic Job Scheduling** - Queues jobs based on resource availability
- **Individual File Support** - Creates separate jobs for each file, preserving folder structure
- **qBittorrent Integration** - Webhook script for automatic job creation
- **REST API** - Complete API for job management and monitoring
- **Retry Logic** - Automatic retry with configurable limits
- **Real-time Progress** - Live download progress with speed and ETA tracking
- **Pushover Notifications** - Alerts for job failures and system events
- **Per-Job Configuration** - Custom bandwidth limits and transfer settings
- **Docker Ready** - Container-optimized with health checks

## Quick Start

### 1. Build and Deploy

```bash
# Clone repository
git clone https://github.com/yourusername/grabarr
cd grabarr

# Build Docker image
make docker-build

# Deploy to remote server
make deploy
```

### 2. Configure

Create `config.yaml` (see [Configuration docs](docs/CONFIGURATION.md) for all options):

```yaml
server:
  port: 8080
  host: "0.0.0.0"

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

logging:
  level: "info"
  format: "json"
```

### 3. Access

- **Web UI**: http://your-server:8080
- **API**: http://your-server:8080/api/v1
- **Health Check**: http://your-server:8080/api/v1/health

## Usage

### Create a Job

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Movie.2024.1080p.mkv",
    "remote_path": "/home/user/torrents/Movie.2024.1080p.mkv",
    "file_size": 2147483648,
    "metadata": {"category": "movies"}
  }'
```

### List Jobs

```bash
curl "http://localhost:8080/api/v1/jobs?status=running&limit=20"
```

### Monitor System

```bash
# System status with gatekeeper info
curl http://localhost:8080/api/v1/status

# Detailed metrics
curl http://localhost:8080/api/v1/metrics
```

## Documentation

### Getting Started

- **[Deployment Guide](docs/DEPLOYMENT.md)** - Docker setup, SSH keys, and deployment workflows
- **[Configuration Reference](docs/CONFIGURATION.md)** - Complete configuration options and examples
- **[qBittorrent Integration](docs/QBITTORRENT.md)** - Webhook script setup and automation

### Core Concepts

- **[Gatekeeper System](docs/GATEKEEPER.md)** - Resource management and admission control
- **[API Reference](docs/API.md)** - Complete REST API documentation

### Development

- **[Development Guide](docs/DEVELOPMENT.md)** - Building, testing, and contributing
- **[Testing Guide](docs/TESTING.md)** - Testing requirements and best practices

## Development

```bash
# Install dependencies
make deps

# Build binary
make build

# Run tests
make test

# Run all pre-commit checks
make test-ci

# Generate mocks
make gen-mocks
```

See [Development Guide](docs/DEVELOPMENT.md) for details.

## Architecture

- **Web UI**: Vanilla JavaScript with responsive CSS
- **API Layer**: HTTP REST API (Gorilla Mux)
- **Queue System**: In-memory queue with SQLite persistence
- **Gatekeeper**: Resource monitoring and admission control
- **Executor**: Rsync-based transfers with SSH key auth
- **Repository**: SQLite database operations
- **Notifications**: Pushover integration

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes and add tests
4. Run `make test-ci` to verify
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

See [Development Guide](docs/DEVELOPMENT.md) for detailed guidelines.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/yourusername/grabarr/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/grabarr/discussions)
- **Documentation**: [docs/](docs/)
