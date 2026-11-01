#!/usr/bin/env bash
# Retry failed jobs from the last N days (default: 2)
# Usage: GRABARR_TOKEN=... ./retry-failed-jobs.sh [base-url] [days]
# Example: GRABARR_TOKEN=... ./retry-failed-jobs.sh https://grabarr.internal.dppeppel.me 2
#
# GRABARR_TOKEN is an API token: docker exec grabarr /app/grabarr token create NAME

set -euo pipefail

: "${GRABARR_TOKEN:?set GRABARR_TOKEN to a grabarr API token}"
ORIGIN="${1:-https://grabarr.internal.dppeppel.me}"
DAYS="${2:-2}"

BASE_URL="${ORIGIN%/}/api/v1"
AUTH=(-H "Authorization: Bearer ${GRABARR_TOKEN}")
SINCE=$(date -u -v-"${DAYS}"d +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "${DAYS} days ago" +"%Y-%m-%dT%H:%M:%SZ")

echo "Fetching failed jobs updated since ${SINCE}..."

# Fetch up to 1000 failed jobs and filter by updated_at
FAILED_IDS=$(curl -sf "${AUTH[@]}" "${BASE_URL}/jobs?status=failed&limit=1000" | \
  jq -r --arg since "$SINCE" \
  '.data[] | select(.updated_at >= $since) | .id')

COUNT=$(echo "$FAILED_IDS" | grep -c '[0-9]' || true)

if [ "$COUNT" -eq 0 ]; then
  echo "No failed jobs found in the last ${DAYS} day(s)."
  exit 0
fi

echo "Found ${COUNT} failed job(s) to retry."

SUCCESS=0
FAIL=0
for ID in $FAILED_IDS; do
  RESPONSE=$(curl -sf "${AUTH[@]}" -X POST "${BASE_URL}/jobs/${ID}/retry" || echo '{"success":false}')
  if echo "$RESPONSE" | jq -e '.success == true' > /dev/null 2>&1; then
    echo "  [OK] Job ${ID} queued for retry"
    SUCCESS=$((SUCCESS + 1))
  else
    echo "  [FAIL] Job ${ID} failed to retry"
    FAIL=$((FAIL + 1))
  fi
done

echo ""
echo "Done: ${SUCCESS} retried, ${FAIL} failed."
