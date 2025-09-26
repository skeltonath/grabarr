# Claude Development Notes

## Project Overview
Grabarr - Go-based download management service for managing downloads from remote seedboxes via rclone.

## Key Infrastructure
- **Development Machine**: Local macOS with Docker
- **Production Server**: Unraid server at `millions.local`
  - SSH: `root@millions.local`
  - Container user: 99:100 (nobody:users)
  - Paths (Host -> Container):
    - Config: `/mnt/user/appdata/grabarr/` -> `/config/`
    - Data: `/mnt/user/appdata/grabarr/data/` -> `/data/`
    - Downloads: `/mnt/user/media/downloads/` -> `/unraid/user/media/downloads/`
    - Cache: `/mnt/cache/` -> `/unraid/cache/`

## Development Workflow
1. Develop and test locally
2. Build container: `docker build --platform linux/amd64 -t grabarr:latest .` (IMPORTANT: Use amd64 for unraid)
3. Save: `docker save grabarr:latest | gzip > grabarr-latest.tar.gz`
4. Transfer: `scp grabarr-latest.tar.gz root@millions.local:/mnt/user/tmp/` (Use /mnt/user/tmp for large files)
5. Load on unraid: `ssh root@millions.local "docker load < /mnt/user/tmp/grabarr-latest.tar.gz"`
6. Deploy using docker-compose:
   ```bash
   # Copy files to unraid appdata directory first
   scp config.yaml docker-compose.yml root@millions.local:/mnt/user/appdata/grabarr/

   # Deploy
   ssh root@millions.local "cd /mnt/user/appdata/grabarr && docker-compose up -d"
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

## Config Management Rules
- **NEVER** add config fields that aren't immediately used in code
- When adding config fields, update config.yaml and the Go structs simultaneously
- Only one config file (config.yaml) to avoid sync issues

## API Testing
```bash
# Create job
curl -X POST http://millions.local:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"name": "test", "remote_path": "/path/to/file", "metadata": {"category": "movies"}}'

# Check status
curl http://millions.local:8080/api/v1/jobs
curl http://millions.local:8080/api/v1/status
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