# Gatekeeper System

Intelligent resource management and job admission control.

## Overview

The Gatekeeper is Grabarr's resource monitoring and admission control system. It ensures jobs only start when sufficient resources are available, preventing:

- Network congestion (bandwidth overload)
- Disk space exhaustion
- Failed transfers due to insufficient space

## How It Works

### Resource Monitoring

Gatekeeper continuously monitors:

1. **Seedbox Bandwidth**: Current transfer speed from active downloads
2. **Cache Disk Space**: Available space on the local cache drive
3. **Resource Availability**: Whether constraints allow new jobs to start

### Job Admission Control

Before starting a job, Gatekeeper performs three checks:

1. **Bandwidth Check**: Is current transfer speed below the configured limit?
2. **Disk Usage Check**: Is cache disk usage below the maximum threshold?
3. **File Size Check**: Will the file fit in available cache space? (optional)

If any check fails, the job remains **queued** and is automatically retried every 5 seconds.

## Configuration

```yaml
gatekeeper:
  seedbox:
    bandwidth_limit_mbps: 500  # Maximum bandwidth in Mbps
    check_interval: "30s"      # How often to check bandwidth usage

  cache_disk:
    path: "/unraid/cache"      # Path to cache disk to monitor
    max_usage_percent: 80      # Maximum cache usage percentage
    check_interval: "30s"      # How often to check disk usage

  rules:
    require_filesize_check: true  # Verify file will fit before starting
```

## Resource Checks Explained

### 1. Bandwidth Check

**Purpose**: Prevents overloading your seedbox connection

**How it works**:
- Queries active rclone transfers for current speed
- Compares against `bandwidth_limit_mbps`
- Blocks new jobs if limit is reached

**Example**:
- Limit: 500 Mbps
- Current usage: 450 Mbps
- Result: ✅ New job allowed (450 < 500)

**Example** (blocked):
- Limit: 500 Mbps
- Current usage: 510 Mbps
- Result: ❌ New job queued (510 >= 500)

**Configuration**:
```yaml
gatekeeper:
  seedbox:
    bandwidth_limit_mbps: 500  # Set based on your connection (e.g., 500 for 1Gbps)
    check_interval: "30s"       # Update frequency
```

**Recommendations**:
- Set limit to 50-80% of your seedbox's total bandwidth
- Example: For 1Gbps seedbox, use 500-800 Mbps
- This leaves headroom for other traffic

### 2. Cache Disk Usage Check

**Purpose**: Prevents filling up your cache drive

**How it works**:
- Checks disk space usage on configured path
- Compares against `max_usage_percent`
- Blocks new jobs if threshold is exceeded

**Example**:
- Max: 80%
- Current: 65%
- Result: ✅ New job allowed (65 < 80)

**Example** (blocked):
- Max: 80%
- Current: 85%
- Result: ❌ New job queued (85 >= 80)

**Configuration**:
```yaml
gatekeeper:
  cache_disk:
    path: "/unraid/cache"       # Path to monitor
    max_usage_percent: 80       # Maximum allowed usage
    check_interval: "30s"       # Update frequency
```

**Recommendations**:
- Set to 75-85% for safety margin
- Lower if you have other cache users
- Monitor trends to tune appropriately

### 3. File Size Check

**Purpose**: Ensures file will fit before starting transfer

**How it works**:
- Requires `file_size` in job creation
- Calculates projected cache usage after download
- Blocks if projected usage exceeds `max_usage_percent`

**Example**:
- Cache: 1TB total, 600GB used (60%), 400GB free
- File size: 100GB
- Projected: 700GB used (70%)
- Max: 80%
- Result: ✅ Allowed (70 < 80)

**Example** (blocked):
- Cache: 1TB total, 700GB used (70%), 300GB free
- File size: 150GB
- Projected: 850GB used (85%)
- Max: 80%
- Result: ❌ Blocked (85 >= 80)

**Configuration**:
```yaml
gatekeeper:
  rules:
    require_filesize_check: true  # Enable pre-flight check
```

**Notes**:
- Only works if `file_size` provided in job creation
- qBittorrent webhook script includes file sizes automatically
- Recommended to leave enabled

## Job States and Flow

### State Transitions

```
[Created] → [Queued] → [Pending] → [Running] → [Completed]
                ↓          ↓            ↓
              [Failed]   [Failed]    [Failed]
```

**Gatekeeper's Role**:

1. **Queued → Pending**: Job enters the queue
2. **Pending → Running**: Gatekeeper checks resources
   - ✅ **Pass**: Job starts executing
   - ❌ **Fail**: Job stays pending, retry in 5s
3. **Automatic Retry**: Scheduler checks pending jobs every 5 seconds

### Example Flow

**Scenario**: 3 jobs queued, bandwidth limit reached

1. **Job A** (queued) → Gatekeeper checks → ❌ Bandwidth limit reached → stays queued
2. **Wait 5 seconds**
3. **Existing transfers complete** → Bandwidth drops
4. **Job A** (queued) → Gatekeeper checks → ✅ Resources available → starts running
5. **Job B** (queued) → Gatekeeper checks → ✅ Resources available → starts running
6. **Job C** (queued) → Gatekeeper checks → ❌ Bandwidth limit reached → stays queued

## Monitoring Gatekeeper

### API: System Status

```bash
curl http://localhost:8080/api/v1/status
```

**Response**:
```json
{
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
```

### API: Metrics

```bash
curl http://localhost:8080/api/v1/metrics
```

**Response includes**:
- Current bandwidth usage vs. limit
- Cache disk usage vs. maximum
- Utilization percentages

### Web UI

The dashboard displays:
- System status card with gatekeeper info
- Real-time resource utilization
- Number of queued jobs waiting for resources

## Tuning Guidelines

### Bandwidth Limits

**Conservative** (high reliability):
```yaml
bandwidth_limit_mbps: 400  # 40% of 1Gbps seedbox
```

**Balanced** (recommended):
```yaml
bandwidth_limit_mbps: 500  # 50% of 1Gbps seedbox
```

**Aggressive** (maximum speed):
```yaml
bandwidth_limit_mbps: 800  # 80% of 1Gbps seedbox
```

### Cache Disk Limits

**Conservative** (safe):
```yaml
max_usage_percent: 75  # Stops at 75%
```

**Balanced** (recommended):
```yaml
max_usage_percent: 80  # Stops at 80%
```

**Aggressive** (risky):
```yaml
max_usage_percent: 90  # Stops at 90% (not recommended)
```

### Check Intervals

**Fast** (responsive, more overhead):
```yaml
check_interval: "10s"  # Checks every 10 seconds
```

**Balanced** (recommended):
```yaml
check_interval: "30s"  # Checks every 30 seconds
```

**Slow** (less overhead, slower response):
```yaml
check_interval: "60s"  # Checks every 60 seconds
```

## Behavior Details

### Graceful Degradation

If resource checks fail (e.g., disk unreachable):
- **Logs error** but **doesn't block jobs**
- Allows operation to continue
- Monitor logs for health issues

### Priority-Based Scheduling

When resources become available:
- Jobs are started in **priority order** (highest first)
- If same priority, **older jobs** run first
- Ensures important jobs aren't starved

### Concurrent Job Limits

Gatekeeper works with job concurrency limits:

1. Check max concurrent jobs (e.g., 5)
2. If under limit, check gatekeeper
3. If gatekeeper allows, start job

**Example**:
- Max concurrent: 5
- Currently running: 3
- Queued jobs: 10
- Gatekeeper: Bandwidth OK, Cache OK
- Result: Start 2 more jobs (up to max of 5)

## Common Scenarios

### Scenario 1: Cache Full

**Situation**:
- Cache: 85% full (exceeds 80% limit)
- 10 jobs queued

**Behavior**:
- All jobs stay queued
- Gatekeeper blocks new starts
- Once cache drops below 80%, jobs resume

**Resolution**:
- Wait for mover to free cache space
- Or manually move files to array
- Or increase `max_usage_percent` (carefully)

### Scenario 2: Bandwidth Saturated

**Situation**:
- Bandwidth: 520 Mbps (exceeds 500 Mbps limit)
- 5 jobs queued

**Behavior**:
- Jobs stay queued until transfers complete
- Bandwidth drops below limit
- Gatekeeper allows next job

**Resolution**:
- Wait for transfers to complete
- Or increase `bandwidth_limit_mbps`
- Or reduce concurrent jobs

### Scenario 3: Large File Won't Fit

**Situation**:
- File: 500GB
- Cache: 1TB total, 600GB used, 400GB free
- Max: 80% (800GB)
- Projected: 1.1TB (110%)

**Behavior**:
- Job stays queued indefinitely
- File will never fit under current constraints

**Resolution**:
- Free up cache space
- Or temporarily increase `max_usage_percent`
- Or download directly to array (skip cache)

## Troubleshooting

### Jobs Stuck in Queued State

**Check**:
1. System status API for resource constraints
2. Logs for gatekeeper messages
3. Cache disk space
4. Bandwidth usage

**Common causes**:
- Cache disk full
- Bandwidth limit reached
- File too large for available space

### Gatekeeper Too Aggressive

**Symptoms**:
- Jobs rarely start
- Resources appear available

**Solutions**:
- Increase `bandwidth_limit_mbps`
- Increase `max_usage_percent`
- Reduce `check_interval` for faster response
- Disable `require_filesize_check` if problematic

### Gatekeeper Too Permissive

**Symptoms**:
- Disk fills up
- Network congested

**Solutions**:
- Decrease `bandwidth_limit_mbps`
- Decrease `max_usage_percent`
- Enable `require_filesize_check`
- Reduce `max_concurrent` jobs

## Advanced Configuration

### Different Limits for Different Categories

Currently not supported, but can be achieved via multiple Grabarr instances:

- Instance 1: Movies (high bandwidth)
- Instance 2: TV (low bandwidth)

### Dynamic Resource Allocation

Currently not supported. Limits are static and require config reload to change.

### External Monitoring Integration

Use the `/api/v1/metrics` endpoint to:
- Feed data to Grafana
- Integrate with Unraid monitoring
- Trigger external alerts

## Best Practices

1. **Start Conservative**: Begin with lower limits and increase gradually
2. **Monitor Trends**: Watch resource usage over time
3. **Leave Headroom**: Don't use 100% of resources
4. **Test Limits**: Temporarily adjust to find optimal settings
5. **Document Changes**: Note why you changed settings
6. **Regular Review**: Revisit settings as usage patterns change

## Related Documentation

- [Configuration Reference](CONFIGURATION.md) - Full config options
- [API Reference](API.md) - Monitoring endpoints
- [Deployment Guide](DEPLOYMENT.md) - Performance tuning
