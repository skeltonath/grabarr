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

# Create jobs: one per file (whether single file or directory)
if [[ -f "$CONTENT_PATH" ]]; then
    # Single file - create one job
    FILE_NAME=$(basename "$CONTENT_PATH")
    REMOTE_PATH="$CONTENT_PATH"
    LOCAL_PATH="$FILE_NAME"
    FILE_SIZE="$SIZE"

    # Build JSON and send
    JSON=$(cat <<JSONEOF
{"name":"${FILE_NAME}","remote_path":"${REMOTE_PATH}","local_path":"${LOCAL_PATH}","file_size":${FILE_SIZE},"metadata":{"category":"${CATEGORY}","torrent_name":"${NAME}"}${DOWNLOAD_CONFIG}}
JSONEOF
)
    curl -X POST "$GRABARR_API_URL" -H "Content-Type: application/json" -H "CF-Access-Client-Id: $GRABARR_CF_CLIENT_ID" -H "CF-Access-Client-Secret: $GRABARR_CF_CLIENT_SECRET" -d "$JSON"

elif [[ -d "$CONTENT_PATH" ]]; then
    # Directory - create job for each file recursively
    # Get the torrent folder name and parent directory
    TORRENT_FOLDER=$(basename "$CONTENT_PATH")
    PARENT_DIR=$(dirname "$CONTENT_PATH")

    while IFS= read -r file_path; do
        FILE_NAME=$(basename "$file_path")
        REMOTE_PATH="$file_path"
        FILE_SIZE=$(stat -c%s "$file_path" 2>/dev/null || echo "0")

        # Calculate relative path from parent directory to preserve folder structure
        # Example: /path/to/Ozark.../Season1/S01E01.mkv -> Ozark.../Season1/S01E01.mkv
        LOCAL_PATH="${file_path#$PARENT_DIR/}"

        # Build JSON and send
        JSON=$(cat <<JSONEOF
{"name":"${FILE_NAME}","remote_path":"${REMOTE_PATH}","local_path":"${LOCAL_PATH}","file_size":${FILE_SIZE},"metadata":{"category":"${CATEGORY}","torrent_name":"${NAME}"}${DOWNLOAD_CONFIG}}
JSONEOF
)
        curl -X POST "$GRABARR_API_URL" -H "Content-Type: application/json" -H "CF-Access-Client-Id: $GRABARR_CF_CLIENT_ID" -H "CF-Access-Client-Secret: $GRABARR_CF_CLIENT_SECRET" -d "$JSON"
    done < <(find "$CONTENT_PATH" -type f)
else
    echo "Error: CONTENT_PATH is neither a file nor directory: $CONTENT_PATH"
    exit 1
fi
