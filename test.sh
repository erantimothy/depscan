#!/bin/bash
set -euo pipefail

BASE_URL="http://localhost:8080"
ROOT_PATH="/home/mugiwara-timoshii/projects/agent-manager"
OUTPUT_DIR="${OUTPUT_DIR:-output}"

SCAN_RESPONSE=$(
  curl -sS -X POST "$BASE_URL/scans" \
    -H 'Content-Type: application/json' \
    -d "{\"rootPath\":\"$ROOT_PATH\"}" \
)

echo "$SCAN_RESPONSE" | jq .

SCAN_ID=$(echo "$SCAN_RESPONSE" | jq -r '.id // empty')

if [ -z "$SCAN_ID" ]; then
  echo "failed to extract scan id"
  exit 1
fi

echo "scan id: $SCAN_ID"

mkdir -p "$OUTPUT_DIR"
SCAN_OUTPUT="$OUTPUT_DIR/scan-$SCAN_ID.json"
curl -fsS "$BASE_URL/scans/$SCAN_ID" | jq . > "$SCAN_OUTPUT"
echo "scan result written to $SCAN_OUTPUT"

jq . "$SCAN_OUTPUT"
curl -sS "$BASE_URL/scans/$SCAN_ID/summary" | jq
curl -sS "$BASE_URL/scans/$SCAN_ID/modules" | jq
curl -sS "$BASE_URL/scans/$SCAN_ID/conflicts" | jq
curl -sS "$BASE_URL/scans/$SCAN_ID/duplicates" | jq
curl -sS "$BASE_URL/scans/$SCAN_ID/graph" | jq

# If you want to compare against an older scan:
# BASE_SCAN_ID="<older-scan-id>"
# curl -sS "$BASE_URL/scans/$SCAN_ID/changes?base=$BASE_SCAN_ID" | jq
