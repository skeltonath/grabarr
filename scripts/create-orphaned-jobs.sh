#!/bin/bash
# Script to create grabarr jobs for movies on whatbox that don't have jobs yet
#
# This script:
# 1. Lists all video files on the whatbox seedbox
# 2. Queries grabarr API for existing jobs
# 3. Creates jobs for any files that don't have jobs yet
#
# Required environment variables:
# GRABARR_API_URL - Grabarr API endpoint URL (e.g., http://millions:8080/api/v1/jobs)
#
# Optional environment variables:
# SEEDBOX_HOST       - SSH host for seedbox (default: whatbox)
# SEEDBOX_BASE_PATH  - Base path to search on seedbox (default: /home/psychomanteum/downloads/completed/dp/)
# CATEGORY           - Category for jobs (default: dp-movies)
# DRY_RUN            - Set to "true" to preview without creating jobs (default: false)

set -euo pipefail

# Load environment variables from config file if it exists
if [[ -f ~/bin/qbt-grabarr.env ]]; then
    source ~/bin/qbt-grabarr.env
fi

# Validate required environment variables
if [[ -z "${GRABARR_API_URL:-}" ]]; then
    echo "Error: GRABARR_API_URL environment variable is required"
    echo "Example: export GRABARR_API_URL=http://millions:8080/api/v1/jobs"
    exit 1
fi

# Set defaults for optional variables
SEEDBOX_HOST="${SEEDBOX_HOST:-whatbox}"
SEEDBOX_BASE_PATH="${SEEDBOX_BASE_PATH:-/home/psychomanteum/downloads/completed/dp/}"
CATEGORY="${CATEGORY:-dp-movies}"
DRY_RUN="${DRY_RUN:-false}"

# Supported video extensions (same as qbt-grabarr.sh)
VIDEO_EXTENSIONS="mkv mp4 avi mov wmv flv webm m4v mpg mpeg ts m2ts"

echo "=== Grabarr Orphaned Jobs Creator ==="
echo "Seedbox: $SEEDBOX_HOST"
echo "Base path: $SEEDBOX_BASE_PATH"
echo "Category: $CATEGORY"
echo "API URL: $GRABARR_API_URL"
if [[ "$DRY_RUN" == "true" ]]; then
    echo "DRY RUN MODE: Will not create jobs"
fi
echo ""

# Step 1: Get all video files from seedbox
echo "Step 1: Listing video files on seedbox..."
TEMP_FILES=$(mktemp)
trap "rm -f $TEMP_FILES" EXIT

# Build find command with all video extensions
FIND_CMD="find \"$SEEDBOX_BASE_PATH\" -type f \\( "
first=true
for ext in $VIDEO_EXTENSIONS; do
    if [[ "$first" == "true" ]]; then
        FIND_CMD="$FIND_CMD-iname \"*.$ext\""
        first=false
    else
        FIND_CMD="$FIND_CMD -o -iname \"*.$ext\""
    fi
done
FIND_CMD="$FIND_CMD \\) -printf '%p|%s\\n'"

# Execute find command on seedbox via SSH
ssh "$SEEDBOX_HOST" "$FIND_CMD" > "$TEMP_FILES"

TOTAL_FILES=$(wc -l < "$TEMP_FILES")
echo "Found $TOTAL_FILES video files on seedbox"
echo ""

# Step 2: Get existing jobs from grabarr API
echo "Step 2: Fetching existing jobs from grabarr API..."
TEMP_JOBS=$(mktemp)
trap "rm -f $TEMP_FILES $TEMP_JOBS" EXIT

# Fetch all jobs (use pagination if needed)
OFFSET=0
LIMIT=1000
TOTAL_JOBS=0

while true; do
    RESPONSE=$(curl -s "${GRABARR_API_URL%/jobs}/jobs?limit=$LIMIT&offset=$OFFSET")

    # Extract remote_path from each job
    echo "$RESPONSE" | jq -r '.data[]?.remote_path // empty' >> "$TEMP_JOBS"

    # Check if there are more pages
    PAGE_COUNT=$(echo "$RESPONSE" | jq -r '.data | length')
    TOTAL_JOBS=$((TOTAL_JOBS + PAGE_COUNT))

    if [[ $PAGE_COUNT -lt $LIMIT ]]; then
        break
    fi

    OFFSET=$((OFFSET + LIMIT))
done

echo "Found $TOTAL_JOBS existing jobs in grabarr"
echo ""

# Step 3: Compare and create missing jobs
echo "Step 3: Creating jobs for orphaned files..."
CREATED_COUNT=0
SKIPPED_COUNT=0
ERROR_COUNT=0

while IFS='|' read -r remote_path file_size; do
    # Check if job already exists
    if grep -Fxq "$remote_path" "$TEMP_JOBS"; then
        ((SKIPPED_COUNT++))
        continue
    fi

    # Extract filename and calculate local_path
    filename=$(basename "$remote_path")
    # Calculate relative path from base path
    relative_path="${remote_path#$SEEDBOX_BASE_PATH}"

    # Build JSON payload
    JSON=$(cat <<JSONEOF
{
  "name": "$filename",
  "remote_path": "$remote_path",
  "local_path": "$relative_path",
  "file_size": $file_size,
  "metadata": {
    "category": "$CATEGORY"
  }
}
JSONEOF
)

    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY RUN] Would create job for: $filename"
        ((CREATED_COUNT++))
    else
        # Create job via API
        HTTP_CODE=$(curl -s -w "%{http_code}" -o /dev/null \
            -X POST "$GRABARR_API_URL" \
            -H "Content-Type: application/json" \
            -d "$JSON")

        if [[ "$HTTP_CODE" == "201" ]]; then
            echo "Created job: $filename"
            ((CREATED_COUNT++))
        else
            echo "ERROR: Failed to create job for $filename (HTTP $HTTP_CODE)"
            ((ERROR_COUNT++))
        fi
    fi
done < "$TEMP_FILES"

# Summary
echo ""
echo "=== Summary ==="
echo "Total files found: $TOTAL_FILES"
echo "Jobs created: $CREATED_COUNT"
echo "Jobs skipped (already exist): $SKIPPED_COUNT"
if [[ $ERROR_COUNT -gt 0 ]]; then
    echo "Errors: $ERROR_COUNT"
fi

if [[ "$DRY_RUN" == "true" ]]; then
    echo ""
    echo "This was a dry run. Set DRY_RUN=false to actually create jobs."
fi
