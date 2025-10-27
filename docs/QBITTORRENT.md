# qBittorrent Integration

Automatically create Grabarr jobs when torrents complete in qBittorrent.

## Overview

The `qbt-grabarr.sh` webhook script integrates qBittorrent with Grabarr, automatically creating download jobs when torrents finish. It supports:

- Individual file jobs (creates one job per file)
- Folder structure preservation
- Per-job download configuration
- Cloudflare Access authentication
- File size tracking for gatekeeper checks

## Installation

### 1. Copy Script to Seedbox

```bash
# Copy from local machine to seedbox
scp scripts/qbt-grabarr.sh user@your-seedbox:~/bin/
chmod +x ~/bin/qbt-grabarr.sh
```

### 2. Create Environment Configuration

On your seedbox, create `~/bin/qbt-grabarr.env`:

```bash
# Grabarr API endpoint
GRABARR_API_URL="https://your-domain.com/api/v1/jobs"

# Cloudflare Access credentials (if using CF Access)
GRABARR_CF_CLIENT_ID="your-client-id"
GRABARR_CF_CLIENT_SECRET="your-client-secret"

# Optional: Per-job download configuration
# Uncomment and adjust values as needed
# GRABARR_TRANSFERS=4
# GRABARR_BW_LIMIT=100M
# GRABARR_BW_LIMIT_FILE=50M
# GRABARR_CHECKERS=8
# GRABARR_MULTI_THREAD_STREAMS=4
```

**Notes**:
- Script automatically sources this file if it exists
- Store sensitive credentials here instead of in the script
- File should be readable only by your user: `chmod 600 ~/bin/qbt-grabarr.env`

### 3. Configure qBittorrent

1. Open qBittorrent Web UI
2. Go to **Tools → Options → Downloads**
3. Scroll to **"Run external program on torrent completion"**
4. Enable the checkbox
5. Set command:

```bash
/bin/bash ~/bin/qbt-grabarr.sh "%N" "%Z" "%L" "%F"
```

**Parameter Explanation**:
- `%N` - Torrent name
- `%Z` - Torrent size (bytes)
- `%L` - Category label
- `%F` - Content path (file or directory)

## How It Works

### Single File Torrents

```
Torrent: Movie.2024.1080p.mkv (2GB)
Path: /home/user/torrents/Movie.2024.1080p.mkv

Result: Creates 1 job
- Name: Movie.2024.1080p.mkv
- Remote path: /home/user/torrents/Movie.2024.1080p.mkv
- Local path: Movie.2024.1080p.mkv
- File size: 2147483648
```

### Multi-File Torrents

```
Torrent: Ozark.S01.1080p (50GB directory)
Path: /home/user/torrents/Ozark.S01.1080p/
Files:
  - Season1/S01E01.mkv (2GB)
  - Season1/S01E02.mkv (2GB)
  - Season1/S01E03.mkv (2GB)

Result: Creates 3 jobs
- Job 1: Season1/S01E01.mkv → Ozark.S01.1080p/Season1/S01E01.mkv
- Job 2: Season1/S01E02.mkv → Ozark.S01.1080p/Season1/S01E02.mkv
- Job 3: Season1/S01E03.mkv → Ozark.S01.1080p/Season1/S01E03.mkv
```

**Key Features**:
- Creates individual job for each file
- Preserves folder structure in `local_path`
- Groups jobs by torrent name (visible in Web UI)

## Script Features

### Individual File Jobs

**Why**: Allows granular control and better progress tracking

**Benefits**:
- See progress for each file individually
- Retry single files without re-downloading entire torrent
- Better resource management (gatekeeper can optimize)
- More accurate ETAs

### Folder Structure Preservation

The script preserves the full folder hierarchy:

```bash
# Torrent structure
/torrents/Show.S01.1080p/
  Season1/
    Episode01.mkv
    Episode02.mkv

# Local result
/downloads/Show.S01.1080p/
  Season1/
    Episode01.mkv
    Episode02.mkv
```

This ensures your media library maintains proper organization.

### Metadata Tracking

Each job includes metadata:

```json
{
  "metadata": {
    "category": "tv",              // qBittorrent category
    "torrent_name": "Show.S01.1080p"  // Original torrent name
  }
}
```

**Benefits**:
- Filter jobs by category in API/Web UI
- Group files from same torrent
- Track source of downloads

### Cloudflare Access Support

If your Grabarr is behind Cloudflare Access, the script includes authentication headers:

```bash
-H "CF-Access-Client-Id: $GRABARR_CF_CLIENT_ID"
-H "CF-Access-Client-Secret: $GRABARR_CF_CLIENT_SECRET"
```

**Setup**:
1. Create service token in Cloudflare Access
2. Add credentials to `~/bin/qbt-grabarr.env`
3. Script automatically includes headers

### File Size Tracking

The script includes file sizes in job creation:

```json
{
  "file_size": 2147483648
}
```

**Benefits**:
- Enables gatekeeper pre-flight checks
- Ensures file will fit before starting download
- Better resource planning

## Download Configuration

### Per-Job Bandwidth Control

Control bandwidth and parallelization by setting environment variables in `~/bin/qbt-grabarr.env`:

```bash
# Bandwidth limits
GRABARR_BW_LIMIT=100M           # Overall bandwidth limit
GRABARR_BW_LIMIT_FILE=50M       # Per-file bandwidth limit

# Parallelization
GRABARR_TRANSFERS=4              # Number of parallel transfers
GRABARR_CHECKERS=8               # Number of file checkers
GRABARR_MULTI_THREAD_STREAMS=4   # Concurrent streams per file
```

These create a `download_config` in each job:

```json
{
  "download_config": {
    "bw_limit": "100M",
    "bw_limit_file": "50M",
    "transfers": 4,
    "checkers": 8,
    "multi_thread_streams": 4
  }
}
```

### Configuration Scenarios

**High Priority (Fast Downloads)**:
```bash
GRABARR_TRANSFERS=8
GRABARR_BW_LIMIT=200M
GRABARR_MULTI_THREAD_STREAMS=8
```

**Low Priority (Background Downloads)**:
```bash
GRABARR_TRANSFERS=1
GRABARR_BW_LIMIT=20M
GRABARR_MULTI_THREAD_STREAMS=1
```

**Balanced (Recommended)**:
```bash
GRABARR_TRANSFERS=4
GRABARR_BW_LIMIT=100M
GRABARR_MULTI_THREAD_STREAMS=4
```

## Category-Based Automation

### qBittorrent Categories

Create categories in qBittorrent to organize torrents:

1. **Tools → Options → Downloads → "Automatically add torrents from..."**
2. Or manually assign categories to torrents

Example categories:
- `movies` - Movie downloads
- `tv` - TV shows
- `anime` - Anime
- `music` - Music

### Grabarr Category Filtering

In `config.yaml`, restrict which categories Grabarr accepts:

```yaml
downloads:
  allowed_categories: ["movies", "tv", "anime"]
```

**Benefits**:
- Only specified categories create jobs
- Reject unwanted content automatically
- Keep downloads organized

## Monitoring

### Check Script Execution

qBittorrent logs script execution in its log file:

```bash
# On seedbox, check qBittorrent logs
tail -f ~/.local/share/data/qBittorrent/logs/qbittorrent.log
```

### Verify Jobs Created

```bash
# Check Grabarr for new jobs
curl http://your-server:8080/api/v1/jobs?limit=10

# Or check Web UI
# Browse to http://your-server:8080
```

### Test Script Manually

```bash
# Run script with test data
~/bin/qbt-grabarr.sh "Test.Movie.2024" "1073741824" "movies" "/path/to/file.mkv"

# Check if job was created
curl http://your-server:8080/api/v1/jobs | grep "Test.Movie.2024"
```

## Troubleshooting

### Script Not Running

**Check**:
1. Script is executable: `ls -la ~/bin/qbt-grabarr.sh`
2. Path is correct in qBittorrent settings
3. qBittorrent log for errors

**Solution**:
```bash
chmod +x ~/bin/qbt-grabarr.sh
```

### Jobs Not Created

**Check**:
1. Environment variables are set
2. API URL is reachable from seedbox
3. Category is allowed (if filtering enabled)

**Test connectivity**:
```bash
# From seedbox
curl -X POST "$GRABARR_API_URL" \
  -H "Content-Type: application/json" \
  -H "CF-Access-Client-Id: $GRABARR_CF_CLIENT_ID" \
  -H "CF-Access-Client-Secret: $GRABARR_CF_CLIENT_SECRET" \
  -d '{"name":"test","remote_path":"/tmp/test","metadata":{"category":"test"}}'
```

### Cloudflare Access Errors

**Error**: "Missing required environment variables"

**Solution**:
Check `~/bin/qbt-grabarr.env` contains:
```bash
GRABARR_CF_CLIENT_ID="..."
GRABARR_CF_CLIENT_SECRET="..."
```

**Error**: "CF-Access denied"

**Solution**:
1. Verify service token is valid
2. Check token has access to application
3. Regenerate token if needed

### Wrong File Paths

**Problem**: Files download to wrong location

**Cause**: Script preserves folder structure from torrent

**Solution**:
- Adjust `local_path` in Grabarr config
- Or modify script to customize path logic

### Duplicate Jobs

**Problem**: Same file creates multiple jobs

**Cause**: Script runs multiple times (qBittorrent bug or manual trigger)

**Solution**:
- Check qBittorrent logs for duplicate executions
- Consider adding job deduplication in Grabarr (future feature)

## Advanced Usage

### Custom Script Modifications

The script is designed to be modified. Common customizations:

**Filter by file type**:
```bash
# Add after line determining file_path
if [[ "$FILE_NAME" != *.mkv ]] && [[ "$FILE_NAME" != *.mp4 ]]; then
    continue  # Skip non-video files
fi
```

**Custom path mapping**:
```bash
# Modify LOCAL_PATH calculation
LOCAL_PATH="custom_prefix/${file_path#$PARENT_DIR/}"
```

**Add custom metadata**:
```bash
# Add to JSON
"metadata": {
  "category": "${CATEGORY}",
  "torrent_name": "${NAME}",
  "custom_field": "custom_value"
}
```

### Multiple Grabarr Instances

Route different categories to different Grabarr instances:

```bash
# In qbt-grabarr.env
if [[ "$CATEGORY" == "movies" ]]; then
    GRABARR_API_URL="https://grabarr-movies.example.com/api/v1/jobs"
elif [[ "$CATEGORY" == "tv" ]]; then
    GRABARR_API_URL="https://grabarr-tv.example.com/api/v1/jobs"
fi
```

### Webhook Alternatives

If you can't use qBittorrent's run-on-completion:

**Option 1**: Use qBittorrent's Web API
- Poll for completed torrents
- Create jobs programmatically

**Option 2**: Watch directory
- Monitor qBittorrent's download folder
- Trigger script on new files

**Option 3**: Manual API calls
- Use curl or Postman to create jobs manually
- Good for testing or one-off downloads

## Best Practices

1. **Test First**: Run script manually before enabling in qBittorrent
2. **Monitor Logs**: Watch qBittorrent and Grabarr logs initially
3. **Start Small**: Test with a small torrent first
4. **Category Filtering**: Use to prevent unwanted downloads
5. **Secure Credentials**: Keep `.env` file permissions at 600
6. **Version Control**: Back up your customized script
7. **Document Changes**: Note any script modifications

## Related Documentation

- [API Reference](API.md) - Job creation endpoint details
- [Configuration](CONFIGURATION.md) - Grabarr config options
- [Gatekeeper](GATEKEEPER.md) - Resource management and file size checks
