#!/bin/bash
# Simple test to see raw SSE output

API_HOST="${API_HOST:-localhost:8080}"
WORKING_DIR="/tmp/test-executor"

mkdir -p "$WORKING_DIR"

echo "Starting execution..."
RESPONSE=$(curl -s -X POST "http://$API_HOST/api/execute" \
    -H "Content-Type: application/json" \
    -d '{
        "prompt": "Say hello world",
        "executor": "claude_code",
        "working_dir": "'"$WORKING_DIR"'"
    }')

SESSION_ID=$(echo "$RESPONSE" | grep -o '"session_id":"[^"]*"' | cut -d'"' -f4)
echo "Session ID: $SESSION_ID"
echo ""
echo "=== Raw SSE output (watch for real-time updates) ==="
echo ""

# Raw curl output - should show data as it arrives
curl -N --no-buffer "http://$API_HOST/api/execute/$SESSION_ID/stream"
