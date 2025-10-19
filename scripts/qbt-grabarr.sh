#!/bin/bash
# qBittorrent to Grabarr webhook script
# Usage: qbt-grabarr.sh "%N" "%Z" "%L" "%F"
# %N = Torrent name, %Z = Size, %L = Category, %F = Content path
#
# Optional environment variables for custom download config:
# GRABARR_TRANSFERS - Number of parallel transfers (default: 1)
# GRABARR_BW_LIMIT - Overall bandwidth limit (default: 10M)
# GRABARR_BW_LIMIT_FILE - Per-file bandwidth limit (default: 10M)
# GRABARR_CHECKERS - Number of checkers (default: 1)
# GRABARR_MULTI_THREAD_STREAMS - Multi-thread streams (default: 1)

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

# Build the complete JSON payload
JSON=$(cat <<JSONEOF
{"name":"${NAME}","remote_path":"${CONTENT_PATH}","file_size":${SIZE},"metadata":{"category":"${CATEGORY}"}${DOWNLOAD_CONFIG}}
JSONEOF
)

curl -X POST https://hooks.dppeppel.me/grabarr/api/v1/jobs   -H "Content-Type: application/json"   -H "CF-Access-Client-Id: 4f5a0916c664d4e5ca803c9232854c6a.access"   -H "CF-Access-Client-Secret: dadf31e21480d113c6c0211649c6ae22de9d3c4dc704ec6868463e40c2e1910d"   -d "$JSON"
