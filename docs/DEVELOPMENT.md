# Development Guide

Guide for developing and contributing to Grabarr.

## Prerequisites

- Go 1.21 or later
- Docker (for containerized testing)
- Make
- Git

## Getting Started

### Clone Repository

```bash
git clone https://github.com/yourusername/grabarr
cd grabarr
```

### Install Dependencies

```bash
make deps
```

This downloads all Go modules and verifies dependencies.

### Build

```bash
# Build binary
make build

# Run locally (requires config.yaml)
make run
```

### Configuration

Create a local config file:

```bash
# Copy example
cp config.example.yaml config.yaml

# Edit with your settings
vim config.yaml
```

See [CONFIGURATION.md](CONFIGURATION.md) for details.

## Development Workflow

### Typical Development Cycle

```bash
# 1. Create feature branch
git checkout -b feature/amazing-feature

# 2. Make changes
# Edit code...

# 3. Format code
make fmt

# 4. Run static analysis
make vet

# 5. Run tests
make test

# 6. Or run all checks at once
make test-ci

# 7. Commit changes
git add .
git commit -m "Add amazing feature"

# 8. Push and create PR
git push origin feature/amazing-feature
```

## Makefile Targets

### Development

| Target | Description |
|--------|-------------|
| `make build` | Build the Go binary |
| `make run` | Build and run locally |
| `make fmt` | Format Go code |
| `make vet` | Run go vet static analysis |
| `make deps` | Download and verify dependencies |
| `make gen-mocks` | Generate test mocks using mockery |
| `make gen-bruno` | Generate Bruno API collection |

### Testing

| Target | Description |
|--------|-------------|
| `make test` | Run all tests |
| `make test-verbose` | Run tests with verbose output |
| `make test-race` | Run tests with race detector |
| `make test-coverage` | Generate HTML coverage report |
| `make test-coverage-summary` | Show coverage percentage |
| `make test-ci` | Run all pre-commit checks (fmt, vet, test) |

### Docker

| Target | Description |
|--------|-------------|
| `make docker-build` | Build Docker image for linux/amd64 |
| `make docker-run` | Build and run with docker-compose |
| `make docker-stop` | Stop running container |
| `make docker-logs` | View container logs |
| `make docker-shell` | Open shell in running container |

### Deployment

| Target | Description |
|--------|-------------|
| `make deploy` | Build, transfer, and deploy to remote server |
| `make deploy-logs` | View logs from remote server |
| `make deploy-restart` | Restart remote service |

### Utility

| Target | Description |
|--------|-------------|
| `make clean` | Clean build artifacts |
| `make help` | Show all available targets |

## Testing

See [TESTING.md](TESTING.md) for comprehensive testing documentation.

### Quick Reference

```bash
# Run all tests
make test

# Run specific package
go test ./internal/queue/...

# Run with coverage
make test-coverage

# Run before committing
make test-ci
```

### Testing Requirements

- Always add tests for new features
- Always update tests when modifying code
- Run `make test-ci` before committing
- Aim for 70%+ coverage on new code
- Use table-driven tests for multiple scenarios
- Mock external dependencies

## Project Structure

```
grabarr/
├── cmd/
│   ├── grabarr/         # Main application entry point
│   │   └── main.go
│   └── bruno-gen/       # Bruno API collection generator
│       └── main.go
├── internal/
│   ├── api/             # HTTP handlers and routes
│   │   ├── handlers.go
│   │   ├── jobs.go
│   │   ├── system.go
│   │   ├── web.go
│   │   └── middleware.go
│   ├── config/          # Configuration management
│   │   ├── config.go
│   │   └── config_test.go
│   ├── executor/        # Job execution (rsync)
│   │   ├── rsync.go
│   │   └── rclone.go (legacy)
│   ├── gatekeeper/      # Resource management
│   │   ├── gatekeeper.go
│   │   └── gatekeeper_test.go
│   ├── interfaces/      # Interface definitions
│   │   └── interfaces.go
│   ├── mocks/           # Generated test mocks
│   ├── models/          # Data models
│   │   ├── job.go
│   │   ├── download_config.go
│   │   └── *_test.go
│   ├── notifications/   # Pushover notifications
│   │   ├── pushover.go
│   │   └── pushover_test.go
│   ├── queue/           # Job queue management
│   │   ├── queue.go
│   │   └── queue_test.go
│   ├── repository/      # Database operations
│   │   ├── repository.go
│   │   ├── schema.sql
│   │   └── repository_test.go
│   ├── rsync/           # Rsync client wrapper
│   │   └── client.go
│   └── testutil/        # Test utilities
│       └── fixtures.go
├── web/
│   └── static/          # Web UI assets
│       ├── index.html
│       ├── css/style.css
│       ├── js/app.js
│       └── images/
├── scripts/
│   └── qbt-grabarr.sh   # qBittorrent webhook script
├── docs/                # Documentation
│   ├── API.md
│   ├── CONFIGURATION.md
│   ├── DEPLOYMENT.md
│   ├── DEVELOPMENT.md  # This file
│   ├── GATEKEEPER.md
│   ├── QBITTORRENT.md
│   └── TESTING.md
├── config.yaml          # Main configuration
├── docker-compose.yml   # Docker deployment
├── Dockerfile           # Container definition
├── Makefile             # Build automation
├── go.mod               # Go dependencies
└── README.md            # Project overview
```

## Architecture

### Components

**API Layer** (`internal/api/`)
- HTTP REST API using Gorilla Mux
- Request validation and response formatting
- Middleware for CORS, logging, content-type
- Web UI serving

**Queue System** (`internal/queue/`)
- In-memory job queue with persistence
- Concurrent job execution
- Retry logic and error handling
- Job lifecycle management

**Gatekeeper** (`internal/gatekeeper/`)
- Resource monitoring (bandwidth, disk)
- Job admission control
- Pre-flight checks

**Executor** (`internal/executor/`)
- Rsync-based file transfers
- Progress tracking
- SSH key authentication

**Repository** (`internal/repository/`)
- SQLite database wrapper
- Job CRUD operations
- Job attempt tracking
- Schema migrations

**Models** (`internal/models/`)
- Data structures and types
- Job states and transitions
- Download configuration

**Notifications** (`internal/notifications/`)
- Pushover integration
- Job failure alerts
- System alerts

### Data Flow

```
[API] → [Queue] → [Gatekeeper] → [Executor] → [Repository]
  ↓                                               ↑
[Web UI]                                   [Database]
```

1. **Job Creation**: API receives job → validates → creates in repository → enqueues
2. **Scheduling**: Queue scheduler checks gatekeeper → if allowed → starts executor
3. **Execution**: Executor transfers file → updates progress → persists to repository
4. **Completion**: Job marked complete → notification sent → cleanup scheduled

### Concurrency

- Queue processes jobs concurrently (configurable limit)
- Gatekeeper monitors resources in background goroutines
- Progress updates use channels for communication
- Repository operations are synchronized with mutexes
- Graceful shutdown ensures jobs are safely queued

## Code Style

### General Guidelines

- Follow standard Go conventions
- Use `gofmt` for formatting (run `make fmt`)
- Use meaningful variable names
- Add comments for exported functions
- Keep functions small and focused
- Prefer composition over inheritance

### Error Handling

```go
// Good: Wrap errors with context
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// Bad: Return raw error
if err := doSomething(); err != nil {
    return err
}
```

### Logging

```go
// Use structured logging
slog.Info("job started", "job_id", job.ID, "name", job.Name)
slog.Error("job failed", "job_id", job.ID, "error", err)

// Not: Printf-style
log.Printf("job %d started: %s", job.ID, job.Name)
```

### Testing

```go
// Use table-driven tests
func TestJobValidation(t *testing.T) {
    tests := []struct {
        name    string
        job     *Job
        wantErr bool
    }{
        {
            name: "valid job",
            job: &Job{Name: "test", RemotePath: "/path"},
            wantErr: false,
        },
        {
            name: "missing name",
            job: &Job{RemotePath: "/path"},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.job.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Adding Features

### 1. Plan

- Discuss in GitHub issue first
- Design API if user-facing
- Consider backwards compatibility
- Update documentation plan

### 2. Implement

- Create feature branch
- Write tests first (TDD)
- Implement feature
- Update related tests
- Add integration tests if needed

### 3. Document

- Add inline code comments
- Update relevant docs in `docs/`
- Add API examples if applicable
- Update CHANGELOG

### 4. Test

```bash
# Run all pre-commit checks
make test-ci

# Check coverage
make test-coverage
```

### 5. Submit

- Create pull request
- Reference related issues
- Describe changes clearly
- Respond to review feedback

## Debugging

### Local Debugging

```bash
# Run with debug logging
# Edit config.yaml:
logging:
  level: "debug"

# Run
make run
```

### Docker Debugging

```bash
# View logs
docker logs -f grabarr

# Open shell
make docker-shell

# Check processes
docker exec grabarr ps aux

# Check files
docker exec grabarr ls -la /config
```

### Database Debugging

```bash
# Open SQLite shell
sqlite3 data/grabarr.db

# View schema
.schema

# Query jobs
SELECT * FROM jobs ORDER BY created_at DESC LIMIT 10;

# Check job attempts
SELECT * FROM job_attempts WHERE job_id = 1;
```

## Common Development Tasks

### Adding a New API Endpoint

1. Define handler in `internal/api/`
2. Register route in `RegisterRoutes()`
3. Add tests in `*_test.go`
4. Document in `docs/API.md`
5. Regenerate Bruno collection: `make gen-bruno`

### Adding a Configuration Option

1. Add field to struct in `internal/config/config.go`
2. Update validation if needed
3. Add to `config.example.yaml`
4. Document in `docs/CONFIGURATION.md`
5. Update tests

### Adding a Database Field

1. Update model in `internal/models/`
2. Update schema in `internal/repository/schema.sql`
3. Add migration logic if needed
4. Update repository methods
5. Add tests

## Generating Code

### Mocks

```bash
# Generate mocks for all interfaces
make gen-mocks

# Configuration is in .mockery.yaml
```

### Bruno API Collection

```bash
# Generate Bruno collection from code
make gen-bruno

# Output: bruno_auto/ directory
```

## CI/CD

### Pre-Commit Checks

Run before every commit:

```bash
make test-ci
```

This runs:
1. `make fmt` - Format code
2. `make vet` - Static analysis
3. `make test` - Run tests

### GitHub Actions (Future)

Planned CI/CD pipeline:
- Lint and format checks
- Run tests with coverage
- Build Docker image
- Integration tests
- Deploy to staging

## Troubleshooting Development Issues

### Build Fails

```bash
# Clean and rebuild
make clean
make deps
make build
```

### Tests Fail

```bash
# Run verbose
make test-verbose

# Run specific test
go test -v -run TestJobCreation ./internal/queue/

# Check for race conditions
make test-race
```

### Mock Generation Fails

```bash
# Install mockery
go install github.com/vektra/mockery/v2@latest

# Regenerate
make gen-mocks
```

## Contributing Guidelines

1. **Code Quality**
   - Follow Go conventions
   - Pass all tests
   - Maintain or improve coverage
   - Run `make test-ci` before committing

2. **Git Workflow**
   - Feature branches from `main`
   - Descriptive commit messages
   - Reference issues in commits
   - Squash commits before merging

3. **Pull Requests**
   - Clear description
   - Include tests
   - Update documentation
   - Respond to reviews promptly

4. **Communication**
   - Discuss major changes in issues first
   - Be respectful and constructive
   - Help others in discussions

## Resources

- [Go Documentation](https://go.dev/doc/)
- [Gorilla Mux](https://github.com/gorilla/mux)
- [SQLite](https://www.sqlite.org/docs.html)
- [Docker Documentation](https://docs.docker.com/)

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/yourusername/grabarr/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/grabarr/discussions)
- **Documentation**: Check `docs/` directory

## Related Documentation

- [Testing Guide](TESTING.md) - Comprehensive testing documentation
- [API Reference](API.md) - API endpoint details
- [Configuration](CONFIGURATION.md) - Configuration options
- [Deployment](DEPLOYMENT.md) - Deployment workflows
