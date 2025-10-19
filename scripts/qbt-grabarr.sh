#!/bin/bash
# qBittorrent to Grabarr webhook script
# Usage: qbt-grabarr.sh "%N" "%Z" "%L" "%F"
# %N = Torrent name, %Z = Size, %L = Category, %F = Content path

NAME="$1"
SIZE="$2"
CATEGORY="$3"
CONTENT_PATH="$4"

JSON=$(cat <<JSONEOF
{"name":"${NAME}","remote_path":"${CONTENT_PATH}","file_size":${SIZE},"metadata":{"category":"${CATEGORY}"}}
JSONEOF
)

curl -X POST https://hooks.dppeppel.me/grabarr/api/v1/jobs   -H "Content-Type: application/json"   -H "CF-Access-Client-Id: 4f5a0916c664d4e5ca803c9232854c6a.access"   -H "CF-Access-Client-Secret: dadf31e21480d113c6c0211649c6ae22de9d3c4dc704ec6868463e40c2e1910d"   -d "$JSON"
