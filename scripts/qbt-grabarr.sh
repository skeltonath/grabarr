#!/bin/bash
# qBittorrent to Grabarr webhook script
# Usage: qbt-grabarr.sh "%N" "%Z" "%L" "%F"
# %N = Torrent name, %Z = Size, %L = Category, %F = Content path
#
# Requirements:
# - Bash 4.0+ (for associative arrays in multi-part RAR filtering)
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
#
# Optional environment variables for archive extraction:
# GRABARR_EXTRACT_ARCHIVES   - Enable automatic extraction of RAR/ZIP files on seedbox (default: true)
# GRABARR_EXTRACT_RETRIES    - Number of extraction retry attempts (default: 2)
# GRABARR_EXTRACT_DELAY      - Seconds to wait between retry attempts (default: 5)
#
# Optional environment variables for file filtering:
# GRABARR_ALLOWED_EXTENSIONS - Space-separated list of allowed file extensions (without dots)
#                              Default: "mkv mp4 avi mov wmv flv webm m4v mpg mpeg ts m2ts srt sub ass ssa idx vtt"
#                              Files not matching these extensions will be skipped
# GRABARR_ALLOWED_PATTERNS   - Space-separated regex patterns for file extensions
#                              Default: "" (empty - no pattern matching by default)
#                              Example: "tmp[0-9]+" would match .tmp01, .tmp99
#                              Patterns are checked before exact extension matching

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

# Set default extraction settings if not configured
if [[ -z "$GRABARR_EXTRACT_ARCHIVES" ]]; then
    GRABARR_EXTRACT_ARCHIVES=true
fi
if [[ -z "$GRABARR_EXTRACT_RETRIES" ]]; then
    GRABARR_EXTRACT_RETRIES=2
fi
if [[ -z "$GRABARR_EXTRACT_DELAY" ]]; then
    GRABARR_EXTRACT_DELAY=5
fi

# Set default allowed extensions if not configured
if [[ -z "$GRABARR_ALLOWED_EXTENSIONS" ]]; then
    GRABARR_ALLOWED_EXTENSIONS="mkv mp4 avi mov wmv flv webm m4v mpg mpeg ts m2ts srt sub ass ssa idx vtt"
fi

# Set default allowed patterns if not configured (empty by default)
if [[ -z "$GRABARR_ALLOWED_PATTERNS" ]]; then
    GRABARR_ALLOWED_PATTERNS=""
fi

# Function to check if a file is an archive
# Usage: is_archive "filename.rar"
# Returns: 0 (true) if archive, 1 (false) if not
is_archive() {
    local filename="$1"
    local extension="${filename##*.}"
    extension=$(echo "$extension" | tr '[:upper:]' '[:lower:]')

    # Check for archive extensions
    case "$extension" in
        rar|zip|r[0-9]|r[0-9][0-9])
            return 0
            ;;
        *)
            # Check for .partN.rar pattern
            if [[ "$filename" =~ \.part[0-9]+\.rar$ ]]; then
                return 0
            fi
            return 1
            ;;
    esac
}

# Function to extract base name from multi-part RAR archive
# Usage: get_archive_basename "movie.r00"
# Returns: "movie" (the base name without RAR extension patterns)
get_archive_basename() {
    local filename="$1"
    local basename="$filename"

    # Remove common multi-part RAR extensions
    # Pattern 1: .rar
    basename="${basename%.rar}"
    basename="${basename%.RAR}"

    # Pattern 1: .r00, .r01, etc.
    basename="${basename%.r[0-9][0-9]}"
    basename="${basename%.R[0-9][0-9]}"

    # Pattern 2: .r1, .r2, etc. (old style, single digit)
    if [[ "$filename" =~ \.[rR][0-9]$ ]]; then
        basename="${basename%.[rR][0-9]}"
    fi

    # Pattern 3: .part1.rar, .part2.rar, etc.
    # Remove .partN.rar pattern - this captures the .part prefix
    if [[ "$basename" =~ \.part[0-9]+ ]]; then
        basename="${basename%.part[0-9]*}"
    fi

    echo "$basename"
}

# Function to determine if a file is the first part of a multi-part RAR archive
# Usage: is_first_rar_part "/path/to/movie.r00"
# Returns: 0 (true) if first part, 1 (false) otherwise
is_first_rar_part() {
    local filepath="$1"
    local filename="$(basename "$filepath")"
    local dirname="$(dirname "$filepath")"

    # Enable case-insensitive matching
    shopt -s nocasematch

    # Pattern 1: *.rar is ALWAYS the first part if it exists (and not .partN.rar)
    # Companion files: .r00, .r01, .r02, etc.
    if [[ "$filename" =~ \.rar$ ]] && [[ ! "$filename" =~ \.part[0-9]+\.rar$ ]]; then
        shopt -u nocasematch
        return 0
    fi

    # Pattern 3: .part1.rar, .part01.rar, .part001.rar, etc.
    # Only part1 is first
    if [[ "$filename" =~ \.part0*1\.rar$ ]]; then
        shopt -u nocasematch
        return 0
    fi

    # Pattern 3: .part2.rar, .part3.rar, etc. are NOT first
    if [[ "$filename" =~ \.part[0-9]+\.rar$ ]]; then
        shopt -u nocasematch
        return 1
    fi

    # Pattern 1 companion parts: .r00, .r01, etc.
    # These are NEVER first if the .rar file exists
    if [[ "$filename" =~ \.r[0-9][0-9]$ ]]; then
        # Check if corresponding .rar exists
        local base="$(get_archive_basename "$filename")"
        if [[ -f "$dirname/$base.rar" ]] || [[ -f "$dirname/$base.RAR" ]]; then
            # The .rar file exists, so this is a companion part
            shopt -u nocasematch
            return 1
        fi
        # If no .rar exists, treat .r00 as first part (orphaned archive)
        if [[ "$filename" =~ \.r00$ ]]; then
            shopt -u nocasematch
            return 0
        fi
        shopt -u nocasematch
        return 1
    fi

    # Pattern 2: .r1, .r2, etc. (old single-digit style)
    # .r1 is the first part
    if [[ "$filename" =~ \.r[0-9]$ ]]; then
        # Check if there's a .rar file (which would take precedence)
        local base="$(get_archive_basename "$filename")"
        if [[ -f "$dirname/$base.rar" ]] || [[ -f "$dirname/$base.RAR" ]]; then
            # The .rar file exists, this is not the first part
            shopt -u nocasematch
            return 1
        fi
        # .r1 is first, .r2+ are not
        if [[ "$filename" =~ \.r1$ ]]; then
            shopt -u nocasematch
            return 0
        fi
        shopt -u nocasematch
        return 1
    fi

    # Not a recognized multi-part pattern
    shopt -u nocasematch
    return 1
}

# Function to filter an array of archive paths to include only first parts and standalone archives
# Usage: filter_first_parts_only archives_array filtered_array
# Modifies: filtered_array (passed by name reference)
# Note: Requires Bash 4+ for associative arrays
filter_first_parts_only() {
    local -n input_array=$1
    local -n output_array=$2

    # Track base names we've already processed to avoid duplicates
    declare -A processed_bases

    for archive in "${input_array[@]}"; do
        local filename="$(basename "$archive")"
        local dirname="$(dirname "$archive")"

        # ZIP files are always extracted (no multi-part support)
        if [[ "$filename" =~ \.[zZ][iI][pP]$ ]]; then
            output_array+=("$archive")
            continue
        fi

        # For RAR files, determine base name and check if first part
        local base="$(get_archive_basename "$filename")"
        local fullpath="$dirname/$base"

        # Skip if we've already processed this base
        if [[ -n "${processed_bases[$fullpath]}" ]]; then
            continue
        fi

        # Check if this is a first part
        if is_first_rar_part "$archive"; then
            output_array+=("$archive")
            processed_bases[$fullpath]=1
        fi
    done
}

# Function to extract archives with retry logic
# Usage: extract_archives "/path/to/content"
# Returns: 0 on success, 1 on failure
extract_archives() {
    local content_path="$1"
    local extraction_attempted=false
    local extraction_failed=false

    echo "Checking for archives in: $content_path" >&2

    # Find all archive files
    local archives=()
    if [[ -f "$content_path" ]]; then
        # Single file torrent
        if is_archive "$(basename "$content_path")"; then
            archives+=("$content_path")
        fi
    elif [[ -d "$content_path" ]]; then
        # Directory torrent - find all archives
        while IFS= read -r -d '' archive; do
            archives+=("$archive")
        done < <(find "$content_path" -type f \( -iname "*.rar" -o -iname "*.zip" -o -iname "*.r[0-9]" -o -iname "*.r[0-9][0-9]" \) -print0)
    fi

    if [[ ${#archives[@]} -eq 0 ]]; then
        echo "No archives found" >&2
        return 0
    fi

    echo "Found ${#archives[@]} archive file(s)" >&2

    # Filter to only first parts of multi-part archives
    local archives_to_extract=()
    filter_first_parts_only archives archives_to_extract

    if [[ ${#archives_to_extract[@]} -eq 0 ]]; then
        echo "No archives to extract (only companion parts found)" >&2
        return 0
    fi

    local skipped_count=$((${#archives[@]} - ${#archives_to_extract[@]}))
    if [[ $skipped_count -gt 0 ]]; then
        echo "Extracting ${#archives_to_extract[@]} archive(s), skipped $skipped_count companion part(s)" >&2
    else
        echo "Extracting ${#archives_to_extract[@]} archive(s)" >&2
    fi
    extraction_attempted=true

    # Extract each archive with retries
    for archive in "${archives_to_extract[@]}"; do
        local archive_dir="$(dirname "$archive")"
        local archive_name="$(basename "$archive")"
        local attempt=0
        local extracted=false

        echo "Extracting: $archive_name" >&2

        while [[ $attempt -le $GRABARR_EXTRACT_RETRIES && $extracted == false ]]; do
            if [[ $attempt -gt 0 ]]; then
                echo "Retry attempt $attempt for $archive_name" >&2
                sleep "$GRABARR_EXTRACT_DELAY"
            fi

            # Try extraction
            if extract_single_archive "$archive" "$archive_dir"; then
                echo "Successfully extracted: $archive_name" >&2
                extracted=true
            else
                echo "Extraction failed for: $archive_name (attempt $((attempt + 1)))" >&2
                ((attempt++))
            fi
        done

        if [[ $extracted == false ]]; then
            echo "Failed to extract $archive_name after $((attempt)) attempts" >&2
            extraction_failed=true
        fi
    done

    if [[ $extraction_failed == true ]]; then
        return 1
    fi

    return 0
}

# Function to extract a single archive
# Usage: extract_single_archive "/path/to/archive.rar" "/extract/to/dir"
# Returns: 0 on success, 1 on failure
extract_single_archive() {
    local archive="$1"
    local dest_dir="$2"
    local extension="${archive##*.}"
    extension=$(echo "$extension" | tr '[:upper:]' '[:lower:]')

    cd "$dest_dir" || return 1

    # Try different extraction tools based on file type
    case "$extension" in
        rar|r[0-9]|r[0-9][0-9])
            # Try unrar first
            if command -v unrar >/dev/null 2>&1; then
                unrar x -o- "$archive" >/dev/null 2>&1 && return 0
            fi
            # Fall back to 7z
            if command -v 7z >/dev/null 2>&1; then
                7z x -y "$archive" >/dev/null 2>&1 && return 0
            fi
            ;;
        zip)
            # Try unzip first
            if command -v unzip >/dev/null 2>&1; then
                unzip -o "$archive" >/dev/null 2>&1 && return 0
            fi
            # Fall back to 7z
            if command -v 7z >/dev/null 2>&1; then
                7z x -y "$archive" >/dev/null 2>&1 && return 0
            fi
            ;;
    esac

    # Check for .partN.rar
    if [[ "$archive" =~ \.part[0-9]+\.rar$ ]]; then
        if command -v unrar >/dev/null 2>&1; then
            unrar x -o- "$archive" >/dev/null 2>&1 && return 0
        fi
        if command -v 7z >/dev/null 2>&1; then
            7z x -y "$archive" >/dev/null 2>&1 && return 0
        fi
    fi

    return 1
}

# Function to check if a file extension is allowed
# Usage: is_extension_allowed "filename.mkv"
# Returns: 0 (true) if allowed, 1 (false) if not allowed
is_extension_allowed() {
    local filename="$1"
    local extension="${filename##*.}"

    # Convert to lowercase for case-insensitive comparison
    extension=$(echo "$extension" | tr '[:upper:]' '[:lower:]')

    # First, check if extension matches any configured regex patterns
    if [[ -n "$GRABARR_ALLOWED_PATTERNS" ]]; then
        for pattern in $GRABARR_ALLOWED_PATTERNS; do
            if [[ "$extension" =~ ^${pattern}$ ]]; then
                return 0
            fi
        done
    fi

    # Then check if extension is in the exact-match allowed list
    for allowed_ext in $GRABARR_ALLOWED_EXTENSIONS; do
        if [[ "$extension" == "$allowed_ext" ]]; then
            return 0
        fi
    done

    return 1
}

NAME="$1"
SIZE="$2"
CATEGORY="$3"
CONTENT_PATH="$4"

# Extract archives if enabled
if [[ "$GRABARR_EXTRACT_ARCHIVES" == "true" ]]; then
    if ! extract_archives "$CONTENT_PATH"; then
        echo "Archive extraction failed after retries, skipping this torrent" >&2
        exit 1
    fi
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

# Create jobs: one per file (whether single file or directory)
if [[ -f "$CONTENT_PATH" ]]; then
    # Single file - create one job
    FILE_NAME=$(basename "$CONTENT_PATH")

    # Check if file extension is allowed
    if ! is_extension_allowed "$FILE_NAME"; then
        echo "Skipping file with disallowed extension: $FILE_NAME" >&2
        exit 0
    fi

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

        # Check if file extension is allowed
        if ! is_extension_allowed "$FILE_NAME"; then
            echo "Skipping file with disallowed extension: $FILE_NAME" >&2
            continue
        fi

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
