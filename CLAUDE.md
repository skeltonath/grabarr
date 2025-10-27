# Claude Development Notes

## Project Overview
Grabarr - Go-based download management service for managing downloads from remote seedboxes via rclone.

## Key Infrastructure
- **Development Machine**: Local macOS with Docker
- **Seedbox**: Whatbox (SSH key authentication)
  - SSH: `ssh whatbox`
  - Script location: `~/bin/qbt-grabarr.sh` (source in `scripts/qbt-grabarr.sh`)
  - qBittorrent "Run on completion" command: `~/bin/qbt-grabarr.sh "%N" "%Z" "%L" "%F"`
- **Production Server**: Unraid server at `millions`
  - SSH: `millions`
  - Container user: 99:100 (nobody:users)
  - Paths (Host -> Container):
    - Config: `/mnt/apps/appdata/grabarr/config/` -> `/config/`
    - Data: `/mnt/apps/appdata/grabarr/data/` -> `/data/`
    - Downloads: `/mnt/user/media/downloads/` -> `/unraid/user/media/downloads/`
    - Cache: `/mnt/cache/` -> `/unraid/cache/`

## Development Workflow
1. Develop and test locally
2. Build container: `docker build --platform linux/amd64 -t grabarr:latest .` (IMPORTANT: Use amd64 for unraid)
3. Save: `docker save grabarr:latest | gzip > grabarr-latest.tar.gz`
4. Transfer: `scp grabarr-latest.tar.gz root@millions:/mnt/user/tmp/` (Use /mnt/user/tmp for large files)
5. Load on unraid: `ssh root@millions "docker load < /mnt/user/tmp/grabarr-latest.tar.gz"`
6. Deploy using docker-compose:
   ```bash
   # Copy files to unraid appdata directory first (config goes in config subdirectory!)
   scp grabarr_rsa root@millions:/mnt/apps/appdata/grabarr/config/grabarr_rsa
   ssh root@millions "chmod 600 /mnt/apps/appdata/grabarr/config/grabarr_rsa && chown 99:100 /mnt/apps/appdata/grabarr/config/grabarr_rsa"
   scp config.yaml root@millions:/mnt/apps/appdata/grabarr/config/
   scp rclone.conf root@millions:/mnt/apps/appdata/grabarr/config/
   scp docker-compose.yml root@millions:/mnt/apps/appdata/grabarr/

   # Deploy
   ssh root@millions "cd /mnt/apps/appdata/grabarr && docker-compose up -d"
   ```

## Critical Docker Notes
- **ALWAYS** build for `--platform linux/amd64` (unraid is x86_64, not ARM)
- **ALWAYS** use `image: grabarr:latest` in docker-compose.yml (NOT `build: .`)
- **Transfer large files** to `/mnt/user/tmp/` not `/tmp` (small root filesystem)
- **Add .dockerignore** to prevent including *.tar.gz files in build context

## Git Workflow
- Feature branches for development
- Merge to main when complete
- Always commit related changes together

## Testing Workflow
- **ALWAYS** add unit tests when adding new features
- **ALWAYS** update tests when modifying existing code
- **ALWAYS** run tests before committing: `make test` or `make test-ci`
- Aim for 70%+ coverage on new code
- Use table-driven tests for multiple scenarios
- Mock external dependencies (HTTP, database, etc.)
- See `TESTING.md` for detailed testing documentation

### Pre-Commit Checklist
```bash
make fmt           # Format code
make vet           # Static analysis
make test          # Run all tests
# OR use the combined command:
make test-ci       # Does all of the above
```

### Testing Commands
```bash
make test                    # Run all tests
make test-verbose            # Run tests with verbose output
make test-race               # Run tests with race detector
make test-coverage           # Generate HTML coverage report
make test-coverage-summary   # Show coverage percentage
make test-ci                 # Run all pre-commit checks
```

## Config Management Rules
- **NEVER** add config fields that aren't immediately used in code
- When adding config fields, update config.yaml and the Go structs simultaneously
- Only one config file (config.yaml) to avoid sync issues

## API Testing
```bash
# Create job
curl -X POST http://millions:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"name": "test", "remote_path": "/path/to/file", "metadata": {"category": "movies"}}'

# Check status
curl http://millions:8080/api/v1/jobs
curl http://millions:8080/api/v1/status
```

## Common Troubleshooting
- Container startup failures: Check config.yaml for missing fields
- File permission issues: Ensure 99:100 ownership in container
- Web UI not found: Files should be at `web/static/index.html`

## Key Dependencies
- rclone (for file transfers)
- sshpass (for SSH operations)
- SQLite (embedded database)
- Go 1.21+

## Environment Variables
- `PUSHOVER_TOKEN`, `PUSHOVER_USER` (notifications)
- `SEEDBOX_HOST`, `SEEDBOX_USER`, `SEEDBOX_PASS` (SSH operations for symlink creation)
- `GRABARR_CONFIG` (config file path override)

## Configuration Files
- `config.yaml` - Main application configuration
- `rclone.conf` - RClone configuration with seedbox credentials (no templating)
- `TODO.md` - Development tasks and feature requests (add TODOs here, not in CLAUDE.md)