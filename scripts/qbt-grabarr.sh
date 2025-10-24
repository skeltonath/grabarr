#!/bin/bash
# qBittorrent to Grabarr webhook script
# Usage: qbt-grabarr.sh "%N" "%Z" "%L" "%F"
# %N = Torrent name, %Z = Size, %L = Category, %F = Content path
#
# Required environment variables:
# GRABARR_API_URL - Grabarr API endpoint URL
# GRABARR_CF_CLIENT_ID - Cloudflare Access Client ID
# GRABARR_CF_CLIENT_SECRET - Cloudflare Access Client Secret
#
# Optional environment variables for custom download config:
# GRABARR_TRANSFERS - Number of parallel transfers (default: 1)
# GRABARR_BW_LIMIT - Overall bandwidth limit (default: 10M)
# GRABARR_BW_LIMIT_FILE - Per-file bandwidth limit (default: 10M)
# GRABARR_CHECKERS - Number of checkers (default: 1)
# GRABARR_MULTI_THREAD_STREAMS - Multi-thread streams (default: 1)

# Load environment variables from config file if it exists
if [[ -f ~/bin/qbt-grabarr.env ]]; then
    source ~/bin/qbt-grabarr.env
fi

# Validate required environment variables
if [[ -z "$GRABARR_API_URL" ]] || [[ -z "$GRABARR_CF_CLIENT_ID" ]] || [[ -z "$GRABARR_CF_CLIENT_SECRET" ]]; then
    echo "Error: Missing required environment variables"
    echo "Please set GRABARR_API_URL, GRABARR_CF_CLIENT_ID, and GRABARR_CF_CLIENT_SECRET"
    exit 1
fi

NAME="$1"
SIZE="$2"
CATEGORY="$3"
CONTENT_PATH="$4"

# Intelligently determine remote path based on folder structure
# Base path for completed downloads on seedbox
BASE_PATH="/home/psychomanteum/downloads/completed/dp/"

# Strip the base path to get relative path
RELATIVE_PATH="${CONTENT_PATH#$BASE_PATH}"

# Check if relative path contains a slash (has folder structure)
if [[ "$RELATIVE_PATH" == *"/"* ]]; then
    # Extract the first directory component (folder name)
    FOLDER="${RELATIVE_PATH%%/*}"
    # Send the folder path (without trailing slash so rsync copies the folder itself)
    REMOTE_PATH="${BASE_PATH}${FOLDER}"
else
    # Single file with no folder structure, use content path as-is
    REMOTE_PATH="$CONTENT_PATH"
fi

# Build download_config JSON if any environment variables are set
DOWNLOAD_CONFIG=""
if [[ -n "$GRABARR_TRANSFERS" ]] || [[ -n "$GRABARR_BW_LIMIT" ]] || [[ -n "$GRABARR_BW_LIMIT_FILE" ]] || \
   [[ -n "$GRABARR_CHECKERS" ]] || [[ -n "$GRABARR_MULTI_THREAD_STREAMS" ]]; then

    CONFIG_PARTS=()

    [[ -n "$GRABARR_TRANSFERS" ]] && CONFIG_PARTS+=("\"transfers\":${GRABARR_TRANSFERS}")
    [[ -n "$GRABARR_CHECKERS" ]] && CONFIG_PARTS+=("\"checkers\":${GRABARR_CHECKERS}")
    [[ -n "$GRABARR_BW_LIMIT" ]] && CONFIG_PARTS+=("\"bw_limit\":\"${GRABARR_BW_LIMIT}\"")
    [[ -n "$GRABARR_BW_LIMIT_FILE" ]] && CONFIG_PARTS+=("\"bw_limit_file\":\"${GRABARR_BW_LIMIT_FILE}\"")
    [[ -n "$GRABARR_MULTI_THREAD_STREAMS" ]] && CONFIG_PARTS+=("\"multi_thread_streams\":${GRABARR_MULTI_THREAD_STREAMS}")

    # Join array elements with commas
    CONFIG_JSON=$(IFS=,; echo "${CONFIG_PARTS[*]}")
    DOWNLOAD_CONFIG=",\"download_config\":{${CONFIG_JSON}}"
fi

# Build the complete JSON payload
JSON=$(cat <<JSONEOF
{"name":"${NAME}","remote_path":"${REMOTE_PATH}","file_size":${SIZE},"metadata":{"category":"${CATEGORY}"}${DOWNLOAD_CONFIG}}
JSONEOF
)

curl -X POST "$GRABARR_API_URL"   -H "Content-Type: application/json"   -H "CF-Access-Client-Id: $GRABARR_CF_CLIENT_ID"   -H "CF-Access-Client-Secret: $GRABARR_CF_CLIENT_SECRET"   -d "$JSON"
