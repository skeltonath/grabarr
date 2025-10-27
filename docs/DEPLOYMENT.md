# Deployment Guide

Complete guide for deploying Grabarr on Docker and Unraid.

## Prerequisites

- Docker and Docker Compose
- SSH access to your seedbox
- Unraid server (or any Linux system with Docker)

## SSH Key Setup

Grabarr uses SSH key authentication to connect to your seedbox.

### 1. Generate SSH Key Pair

```bash
# Generate RSA key without passphrase (required for automation)
ssh-keygen -t rsa -b 4096 -f grabarr_rsa -N ""
```

This creates two files:
- `grabarr_rsa` - Private key (keep secure)
- `grabarr_rsa.pub` - Public key (copy to seedbox)

### 2. Copy Public Key to Seedbox

```bash
# Option 1: Using ssh-copy-id (easiest)
ssh-copy-id -i grabarr_rsa.pub user@your-seedbox.example.com

# Option 2: Manual copy
cat grabarr_rsa.pub | ssh user@your-seedbox.example.com "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys"
```

### 3. Test SSH Connection

```bash
# Test that key-based auth works
ssh -i grabarr_rsa user@your-seedbox.example.com "echo 'Connection successful'"
```

### 4. Deploy Private Key

For Unraid:

```bash
# Copy private key to appdata
sudo cp grabarr_rsa /mnt/apps/appdata/grabarr/config/
sudo chmod 600 /mnt/apps/appdata/grabarr/config/grabarr_rsa
sudo chown 99:100 /mnt/apps/appdata/grabarr/config/grabarr_rsa
```

For other systems:

```bash
# Copy to your chosen config directory
cp grabarr_rsa /path/to/config/
chmod 600 /path/to/config/grabarr_rsa
```

## Docker Setup

### Directory Structure

Create required directories:

```bash
# For Unraid
sudo mkdir -p /mnt/apps/appdata/grabarr/{config,data}
sudo chown -R 99:100 /mnt/apps/appdata/grabarr

# For other systems
mkdir -p ~/grabarr/{config,data}
```

### Configuration Files

1. **config.yaml** - See [CONFIGURATION.md](CONFIGURATION.md) for details
2. **.env** - Environment variables
3. **docker-compose.yml** - Container definition

Place all three files in `/mnt/apps/appdata/grabarr/` (Unraid) or your chosen directory.

### docker-compose.yml

```yaml
services:
  grabarr:
    image: grabarr:latest
    container_name: grabarr
    restart: unless-stopped
    ports:
      - "8080:8080"
    user: "99:100"  # Unraid nobody:users (change for other systems)
    volumes:
      # Configuration and data
      - /mnt/apps/appdata/grabarr/config:/config
      - /mnt/apps/appdata/grabarr/data:/data

      # Unraid mount points (adjust for your system)
      - /mnt/user:/unraid/user:rw        # Main array
      - /mnt/cache:/unraid/cache:rw      # Cache drive

    env_file:
      - .env

    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "5"

    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/api/v1/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 30s

    networks:
      - proxynet

networks:
  proxynet:
    external: true
```

**Notes:**
- Adjust `user` if not on Unraid (use `id -u:id -g`)
- Adjust volume paths for your system
- Remove `networks` section if not using reverse proxy

## Building the Image

### Local Build

```bash
# Clone repository
git clone https://github.com/yourusername/grabarr
cd grabarr

# Build for linux/amd64 (Unraid/x86_64)
make docker-build

# Or manually
docker build --platform linux/amd64 -t grabarr:latest .
```

### Transfer to Remote Server

```bash
# Save image
docker save grabarr:latest | gzip > grabarr-latest.tar.gz

# Copy to server (use /mnt/user/tmp for large files on Unraid)
scp grabarr-latest.tar.gz root@your-server:/mnt/user/tmp/

# Load on server
ssh root@your-server "docker load < /mnt/user/tmp/grabarr-latest.tar.gz"
```

## Deployment

### Using Make (Automated)

The Makefile provides automated deployment:

```bash
# Deploy everything (build + transfer + start)
make deploy

# View remote logs
make deploy-logs

# Restart remote service
make deploy-restart
```

**Makefile Configuration:**

Edit `Makefile` to set your server:

```makefile
REMOTE_HOST=millions        # Your server hostname
REMOTE_USER=root            # SSH user
```

### Manual Deployment

```bash
# 1. Transfer files
scp docker-compose.yml root@your-server:/mnt/apps/appdata/grabarr/
scp config.yaml root@your-server:/mnt/apps/appdata/grabarr/config/
scp .env root@your-server:/mnt/apps/appdata/grabarr/
scp grabarr_rsa root@your-server:/mnt/apps/appdata/grabarr/config/

# 2. Set permissions
ssh root@your-server "chmod 600 /mnt/apps/appdata/grabarr/config/grabarr_rsa && \
                       chown 99:100 /mnt/apps/appdata/grabarr/config/grabarr_rsa"

# 3. Transfer and load Docker image
docker save grabarr:latest | gzip | \
  ssh root@your-server "gunzip | docker load"

# 4. Start service
ssh root@your-server "cd /mnt/apps/appdata/grabarr && docker-compose up -d"
```

## Starting the Service

```bash
# Start
docker-compose up -d

# View logs
docker-compose logs -f

# Stop
docker-compose down

# Restart
docker-compose restart
```

## Verification

After deployment, verify the service is running:

```bash
# Check container status
docker ps | grep grabarr

# Check logs
docker logs grabarr

# Test API
curl http://your-server:8080/api/v1/health

# Access Web UI
# Open browser to: http://your-server:8080
```

## Reverse Proxy (Optional)

### Nginx

```nginx
server {
    listen 80;
    server_name grabarr.yourdomain.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Caddy

```caddy
grabarr.yourdomain.com {
    reverse_proxy localhost:8080
}
```

### Cloudflare Tunnel

```yaml
# cloudflared config.yml
ingress:
  - hostname: grabarr.yourdomain.com
    service: http://localhost:8080
  - service: http_status:404
```

## Updating

### Update Process

```bash
# 1. Pull latest code
git pull

# 2. Rebuild image
make docker-build

# 3. Deploy update
make deploy

# Or manually
docker-compose down
docker-compose up -d
```

### Zero-Downtime Updates

For zero-downtime updates, use Docker's rolling update:

```bash
# Build new image with tag
docker build -t grabarr:v2 .

# Update docker-compose.yml to use new tag
# Then rolling update
docker-compose up -d
```

## Backup

### What to Backup

1. **Database**: `/mnt/apps/appdata/grabarr/data/grabarr.db`
2. **Configuration**: `/mnt/apps/appdata/grabarr/config/config.yaml`
3. **SSH Key**: `/mnt/apps/appdata/grabarr/config/grabarr_rsa`
4. **Environment**: `/mnt/apps/appdata/grabarr/.env`

### Backup Script

```bash
#!/bin/bash
BACKUP_DIR="/mnt/user/backups/grabarr"
DATE=$(date +%Y%m%d)

mkdir -p "$BACKUP_DIR"

# Backup database
cp /mnt/apps/appdata/grabarr/data/grabarr.db "$BACKUP_DIR/grabarr-${DATE}.db"

# Backup config
tar -czf "$BACKUP_DIR/grabarr-config-${DATE}.tar.gz" \
  /mnt/apps/appdata/grabarr/config \
  /mnt/apps/appdata/grabarr/.env \
  /mnt/apps/appdata/grabarr/docker-compose.yml

# Keep last 7 days
find "$BACKUP_DIR" -name "grabarr-*" -mtime +7 -delete
```

### Restore

```bash
# Stop service
docker-compose down

# Restore database
cp grabarr-20240115.db /mnt/apps/appdata/grabarr/data/grabarr.db

# Restore config
tar -xzf grabarr-config-20240115.tar.gz -C /

# Start service
docker-compose up -d
```

## Monitoring

### Health Checks

```bash
# Docker health status
docker inspect grabarr | grep -A 10 Health

# API health check
curl http://localhost:8080/api/v1/health

# System status
curl http://localhost:8080/api/v1/status
```

### Logs

```bash
# Follow logs
docker logs -f grabarr

# Last 100 lines
docker logs --tail 100 grabarr

# Since timestamp
docker logs --since 2024-01-15T10:00:00 grabarr

# Container stats
docker stats grabarr
```

## Troubleshooting

### Container Won't Start

```bash
# Check logs
docker logs grabarr

# Common issues:
# - Config file not found
# - Invalid config syntax
# - Port already in use
# - Permission issues
```

### SSH Connection Fails

```bash
# Test from container
docker exec grabarr ssh -i /config/grabarr_rsa user@seedbox "echo test"

# Check key permissions
docker exec grabarr ls -la /config/grabarr_rsa
# Should be: -rw------- 1 99 100

# Check key is in authorized_keys on seedbox
ssh user@seedbox "cat ~/.ssh/authorized_keys | grep $(cat grabarr_rsa.pub)"
```

### Permission Errors

```bash
# Fix ownership (Unraid)
chown -R 99:100 /mnt/apps/appdata/grabarr

# Fix permissions
chmod 600 /mnt/apps/appdata/grabarr/config/grabarr_rsa
chmod 644 /mnt/apps/appdata/grabarr/config/config.yaml
chmod 600 /mnt/apps/appdata/grabarr/.env
```

### Database Errors

```bash
# Check database file
ls -la /mnt/apps/appdata/grabarr/data/grabarr.db

# Check database integrity
docker exec grabarr sqlite3 /data/grabarr.db "PRAGMA integrity_check;"

# Backup and recreate (WARNING: loses all data)
docker-compose down
mv /mnt/apps/appdata/grabarr/data/grabarr.db ~/grabarr.db.backup
docker-compose up -d
```

### Web UI Not Accessible

```bash
# Check container is running
docker ps | grep grabarr

# Check port mapping
docker port grabarr

# Test from server
curl http://localhost:8080

# Check firewall
# Unraid: Settings → Network → Docker → Custom Network
```

## Performance Tuning

### Resource Limits

Add to docker-compose.yml:

```yaml
services:
  grabarr:
    # ... other settings ...
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 512M
        reservations:
          cpus: '1'
          memory: 256M
```

### Disk I/O

For better performance on Unraid:

- Use cache for database: `/mnt/cache/grabarr/data`
- Downloads go to array: `/mnt/user/media/downloads`

### Network

- Use Docker bridge network (not host) for security
- Use reverse proxy for SSL termination
- Consider VPN container for seedbox connection

## Security Hardening

1. **Non-root user**: Already using 99:100
2. **Read-only root**: Add to docker-compose.yml:
   ```yaml
   read_only: true
   tmpfs:
     - /tmp
   ```
3. **No new privileges**:
   ```yaml
   security_opt:
     - no-new-privileges:true
   ```
4. **Reverse proxy authentication**: Use nginx basic auth or Cloudflare Access
5. **Network isolation**: Use Docker networks

## Unraid-Specific Tips

### Community Applications

Consider creating a Community Applications template for easier installation.

### Notifications

Integrate with Unraid's notification system using Pushover (Unraid supports it natively).

### Array Management

- Downloads go to `/mnt/user/media/downloads` (array)
- Database on `/mnt/cache/grabarr/data` (cache)
- This optimizes for Unraid's cache/array architecture

### Docker Network

If using reverse proxy:

```bash
# Create custom network
docker network create proxynet

# Add to both grabarr and proxy containers
```

## Next Steps

- [Configure qBittorrent integration](QBITTORRENT.md)
- [Learn about the Gatekeeper system](GATEKEEPER.md)
- [Explore the API](API.md)
