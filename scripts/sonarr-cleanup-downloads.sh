#!/bin/bash

# Cleanup script for Sonarr post-import
# Triggered on OnImportComplete.
#
# Deletes only the episode files Sonarr actually imported, then removes the
# whole download folder once no unimported video files remain.
#
# Background: Sonarr matches a torrent to a single season. A multi-season pack
# imports as one season and fires OnImportComplete once, with Sonarr_SourcePath
# set to the *release folder*. Deleting that folder outright destroys the
# seasons Sonarr never imported.
#
# Sonarr provides no list of source paths on this event (and sceneName is often
# null), so imported files are identified by season/episode numbers parsed from
# the filenames:
#   Sonarr_EpisodeFile_SeasonNumber   e.g. 2
#   Sonarr_EpisodeFile_EpisodeNumbers e.g. 1,2,3
#
# Set CLEANUP_DRY_RUN=1 to log actions without deleting.

set -euo pipefail

VIDEO_EXT="mkv|mp4|avi|m4v|mpg|mpeg|wmv|flv|mov|ts|m2ts"
COMPANION_EXT="srt|sub|idx|ass|ssa|nfo|sup"
DRY_RUN="${CLEANUP_DRY_RUN:-0}"

log() { echo "[cleanup] $*"; }

run() {
    if [ "$DRY_RUN" = "1" ]; then
        log "DRY_RUN would: $*"
    else
        "$@"
    fi
}

# Handle test events - exit successfully without doing anything
if [[ "${sonarr_eventtype:-}" == "Test" ]]; then
    exit 0
fi

SOURCE_PATH="${sonarr_sourcepath:-}"

if [ -z "$SOURCE_PATH" ]; then
    echo "Error: No source path found in environment variables" >&2
    exit 1
fi

# Safety checks
# 1. Only clean within /media/downloads
if [[ "$SOURCE_PATH" != /media/downloads/* ]]; then
    echo "Error: Path $SOURCE_PATH is outside /media/downloads" >&2
    exit 1
fi

# 2. Never delete /media/downloads itself
if [[ "$SOURCE_PATH" == "/media/downloads" ]]; then
    echo "Error: Refusing to delete /media/downloads directory itself" >&2
    exit 1
fi

# Exit silently if already cleaned up
if [ ! -e "$SOURCE_PATH" ]; then
    exit 0
fi

# A single-file download has nothing else to protect - remove it directly.
if [ -f "$SOURCE_PATH" ]; then
    run rm -f "$SOURCE_PATH"
    log "removed file $SOURCE_PATH"
    exit 0
fi

if [ ! -d "$SOURCE_PATH" ]; then
    echo "Error: $SOURCE_PATH exists but is neither a file nor directory" >&2
    exit 1
fi

SEASON="${sonarr_episodefile_seasonnumber:-}"
EPISODES="${sonarr_episodefile_episodenumbers:-}"

# Without season/episode numbers we cannot tell imported files from the rest.
# Deleting the folder here is what destroyed the unimported seasons, so bail out
# and leave the download in place instead.
if [ -z "$SEASON" ] || [ -z "$EPISODES" ]; then
    log "no season/episode numbers in environment; leaving $SOURCE_PATH untouched"
    exit 0
fi

# Build an alternation of episode codes for the imported season, covering both
# zero-padded and bare numbering (s02e01 / s2e1) case-insensitively.
season_padded=$(printf '%02d' "$SEASON" 2>/dev/null || echo "$SEASON")
codes=""
IFS=',' read -ra ep_list <<< "$EPISODES"
for ep in "${ep_list[@]}"; do
    ep="${ep//[[:space:]]/}"
    [ -z "$ep" ] && continue
    ep_padded=$(printf '%02d' "$ep" 2>/dev/null || echo "$ep")
    for s in "$season_padded" "$SEASON"; do
        for e in "$ep_padded" "$ep"; do
            codes+="|s${s}e${e}"
        done
    done
done
codes="${codes#|}"

if [ -z "$codes" ]; then
    log "could not build episode match pattern; leaving $SOURCE_PATH untouched"
    exit 0
fi

# Classify every file in one pass. Matching is done with bash [[ =~ ]] (POSIX
# ERE) rather than `find -iregex`: GNU find defaults to Emacs regex and BSD find
# to BRE, and in both "(a|b)" is a literal, not an alternation.
shopt -s nocasematch

imported=()
remaining=0
while IFS= read -r -d '' f; do
    base="${f##*/}"
    ext="${base##*.}"

    is_video=0
    [[ "$ext" =~ ^(${VIDEO_EXT})$ ]] && is_video=1
    is_companion=0
    [[ "$ext" =~ ^(${COMPANION_EXT})$ ]] && is_companion=1
    [ "$is_video" -eq 0 ] && [ "$is_companion" -eq 0 ] && continue

    if [[ "$base" =~ (^|[^0-9])(${codes})([^0-9]|$) ]]; then
        imported+=("$f")
    elif [ "$is_video" -eq 1 ]; then
        remaining=$((remaining + 1))
    fi
done < <(find "$SOURCE_PATH" -type f -print0)

# Delete the imported episodes (and their companion subtitle/nfo files).
if [ "${#imported[@]}" -gt 0 ]; then
    for f in "${imported[@]}"; do
        run rm -f "$f"
        log "removed imported file: ${f#"$SOURCE_PATH"/}"
    done
fi

log "removed ${#imported[@]} file(s) for season $SEASON episodes $EPISODES"

if [ "$remaining" -eq 0 ]; then
    run rm -rf "$SOURCE_PATH"
    log "no unimported video files left; removed $SOURCE_PATH"
else
    log "$remaining unimported video file(s) remain; keeping $SOURCE_PATH"
fi

exit 0
