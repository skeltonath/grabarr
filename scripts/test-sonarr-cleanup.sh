#!/bin/bash
# Test harness for sonarr-cleanup-downloads.sh
# Simulates the Boy Meets World multi-season pack scenario.
set -uo pipefail

SCRIPT="/Users/dppeppel/projects/grabarr/scripts/sonarr-cleanup-downloads.sh"
ROOT=$(mktemp -d)
PASS=0; FAIL=0

# The script hardcodes /media/downloads; fake that root inside the sandbox.
DL="$ROOT/media/downloads"

check() { # check <desc> <expected> <actual>
  if [ "$2" = "$3" ]; then echo "  PASS: $1"; PASS=$((PASS+1));
  else echo "  FAIL: $1 (expected '$2', got '$3')"; FAIL=$((FAIL+1)); fi
}

build_pack() {
  local base="$1"; rm -rf "$base"; mkdir -p "$base"
  for s in 1 2 3 4 5 6 7; do
    mkdir -p "$base/Season $s"
    for e in $(seq 1 22); do
      printf '%02d' >/dev/null
      touch "$base/Season $s/Boy Meets World - S$(printf '%02d' $s)E$(printf '%02d' $e) - Ep (480p x265 EDGE2020).mkv"
    done
  done
}

# Run the script with a patched /media/downloads root
run_script() {
  sed "s#/media/downloads#$DL#g" "$SCRIPT" > "$ROOT/cleanup.sh"
  chmod +x "$ROOT/cleanup.sh"
  "$ROOT/cleanup.sh" "$@"
}

echo "TEST 1: multi-season pack, Sonarr imports only Season 1"
PACK="$DL/Boy Meets World (1993) Season 1-7 S01-07 (480p DSNP WEBRIP x265 HEVC 10bit AAC 2.0 EDGE2020)"
build_pack "$PACK"
before=$(find "$PACK" -name '*.mkv' | wc -l | tr -d ' ')
sonarr_eventtype=Download \
sonarr_sourcepath="$PACK" \
sonarr_episodefile_seasonnumber=1 \
sonarr_episodefile_episodenumbers="$(seq -s, 1 22)" \
run_script >/dev/null 2>&1
after=$(find "$PACK" -name '*.mkv' 2>/dev/null | wc -l | tr -d ' ')
s1=$(find "$PACK/Season 1" -name '*.mkv' 2>/dev/null | wc -l | tr -d ' ')
check "starts with 154 episodes" "154" "$before"
check "Season 1 files deleted" "0" "$s1"
check "seasons 2-7 preserved (132 files)" "132" "$after"
check "pack folder still exists" "yes" "$([ -d "$PACK" ] && echo yes || echo no)"

echo
echo "TEST 2: final season imported -> folder fully removed"
for s in 2 3 4 5 6 7; do
  sonarr_eventtype=Download \
  sonarr_sourcepath="$PACK" \
  sonarr_episodefile_seasonnumber=$s \
  sonarr_episodefile_episodenumbers="$(seq -s, 1 22)" \
  run_script >/dev/null 2>&1
done
check "pack folder removed after last season" "no" "$([ -d "$PACK" ] && echo yes || echo no)"

echo
echo "TEST 3: single-season torrent (normal case) -> folder removed"
P2="$DL/Some.Show.S03.1080p.WEB-DL"
rm -rf "$P2"; mkdir -p "$P2"
for e in 1 2 3; do touch "$P2/Some.Show.S03E0$e.1080p.WEB-DL.mkv"; done
touch "$P2/some.nfo"
sonarr_eventtype=Download \
sonarr_sourcepath="$P2" \
sonarr_episodefile_seasonnumber=3 \
sonarr_episodefile_episodenumbers="1,2,3" \
run_script >/dev/null 2>&1
check "single-season folder removed" "no" "$([ -d "$P2" ] && echo yes || echo no)"

echo
echo "TEST 4: missing env vars -> refuse to delete anything"
P3="$DL/Mystery.Pack"
rm -rf "$P3"; mkdir -p "$P3"; touch "$P3/a.mkv" "$P3/b.mkv"
sonarr_eventtype=Download sonarr_sourcepath="$P3" run_script >/dev/null 2>&1
check "folder untouched without season/ep vars" "2" "$(find "$P3" -name '*.mkv' | wc -l | tr -d ' ')"

echo
echo "TEST 5: path outside /media/downloads -> rejected"
OUT="$ROOT/somewhere/else"; mkdir -p "$OUT"; touch "$OUT/x.mkv"
sonarr_eventtype=Download sonarr_sourcepath="$OUT" \
  sonarr_episodefile_seasonnumber=1 sonarr_episodefile_episodenumbers=1 \
  run_script >/dev/null 2>&1
rc=$?
check "non-downloads path rejected (exit 1)" "1" "$rc"
check "outside file untouched" "1" "$(find "$OUT" -name '*.mkv' | wc -l | tr -d ' ')"

echo
echo "TEST 6: subtitles alongside imported episode are removed"
P4="$DL/Show.S01"
rm -rf "$P4"; mkdir -p "$P4"
touch "$P4/Show.S01E01.mkv" "$P4/Show.S01E01.srt" "$P4/Show.S01E02.mkv"
sonarr_eventtype=Download sonarr_sourcepath="$P4" \
  sonarr_episodefile_seasonnumber=1 sonarr_episodefile_episodenumbers="1" \
  run_script >/dev/null 2>&1
check "E01 subtitle removed with episode" "no" "$([ -f "$P4/Show.S01E01.srt" ] && echo yes || echo no)"
check "E02 preserved" "yes" "$([ -f "$P4/Show.S01E02.mkv" ] && echo yes || echo no)"

echo
echo "TEST 7: dry run deletes nothing"
P5="$DL/Dry.S01"
rm -rf "$P5"; mkdir -p "$P5"; touch "$P5/Dry.S01E01.mkv" "$P5/Dry.S01E02.mkv"
CLEANUP_DRY_RUN=1 sonarr_eventtype=Download sonarr_sourcepath="$P5" \
  sonarr_episodefile_seasonnumber=1 sonarr_episodefile_episodenumbers="1,2" \
  run_script >/dev/null 2>&1
check "dry run left both files" "2" "$(find "$P5" -name '*.mkv' | wc -l | tr -d ' ')"
check "dry run left folder" "yes" "$([ -d "$P5" ] && echo yes || echo no)"

echo
echo "=============================="
echo "PASSED: $PASS   FAILED: $FAIL"
rm -rf "$ROOT"
[ "$FAIL" -eq 0 ]
