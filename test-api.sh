#!/bin/bash

# Test script for Vibe Kanban Go API
# Usage: ./test-api.sh [options]
#   -p, --prompt      Prompt to execute (default: "Create a simple hello.go file that prints 'Hello World'")
#   -d, --dir         Working directory (default: /tmp/test-executor)
#   -e, --executor    Executor to use (default: claude_code)
#   -h, --help        Show this help message

set -e

# Default configuration
API_HOST="${API_HOST:-localhost:8080}"
PROMPT="Create a simple hello.go file that prints 'Hello World'"
WORKING_DIR="/tmp/test-executor"
EXECUTOR="claude_code"

# Parse command line options
while [[ $# -gt 0 ]]; do
    case $1 in
        -p|--prompt)
            PROMPT="$2"
            shift 2
            ;;
        -d|--dir)
            WORKING_DIR="$2"
            shift 2
            ;;
        -e|--executor)
            EXECUTOR="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: ./test-api.sh [options]"
            echo "  -p, --prompt      Prompt to execute"
            echo "  -d, --dir         Working directory (default: /tmp/test-executor)"
            echo "  -e, --executor    Executor to use (default: claude_code)"
            echo "  -h, --help        Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Vibe Kanban Go API Test ===${NC}"
echo ""
echo "Configuration:"
echo "  API Host: $API_HOST"
echo "  Prompt: $PROMPT"
echo "  Working Dir: $WORKING_DIR"
echo "  Executor: $EXECUTOR"
echo ""

if command -v jq >/dev/null 2>&1; then
    JQ_AVAILABLE=1
else
    JQ_AVAILABLE=0
    echo -e "${YELLOW}Warning: jq not found, stream data will use raw output${NC}"
fi

# Check if working directory exists, create if not
if [ ! -d "$WORKING_DIR" ]; then
    echo -e "${YELLOW}Creating working directory: $WORKING_DIR${NC}"
    mkdir -p "$WORKING_DIR"
fi

# Check if API is running
echo -e "${YELLOW}Checking API health...${NC}"
if ! curl -s --max-time 5 "http://$API_HOST/health" > /dev/null 2>&1; then
    echo -e "${RED}Error: API is not running at http://$API_HOST${NC}"
    echo "Please start the API server first:"
    echo "  cd go-api && go run cmd/server/main.go"
    exit 1
fi
echo -e "${GREEN}API is healthy!${NC}"
echo ""

# Build env object from environment variables
ENV_VARS=""
for var in ANTHROPIC_AUTH_TOKEN ANTHROPIC_BASE_URL ANTHROPIC_DEFAULT_HAIKU_MODEL ANTHROPIC_DEFAULT_OPUS_MODEL ANTHROPIC_DEFAULT_SONNET_MODEL ANTHROPIC_MODEL; do
    val="${!var}"
    if [ -n "$val" ]; then
        if [ -n "$ENV_VARS" ]; then
            ENV_VARS="$ENV_VARS,"
        fi
        ENV_VARS="$ENV_VARS\"$var\":\"$val\""
    fi
done

# Execute request
echo -e "${YELLOW}Sending execution request...${NC}"
echo "  Prompt: $PROMPT"
echo "  Executor: $EXECUTOR"
if [ -n "$ENV_VARS" ]; then
    echo "  Env: {$ENV_VARS}"
fi
echo ""

if [ -n "$ENV_VARS" ]; then
    REQUEST_BODY="{\"prompt\": \"$PROMPT\", \"executor\": \"$EXECUTOR\", \"working_dir\": \"$WORKING_DIR\", \"env\": {$ENV_VARS}}"
else
    REQUEST_BODY="{\"prompt\": \"$PROMPT\", \"executor\": \"$EXECUTOR\", \"working_dir\": \"$WORKING_DIR\"}"
fi

RESPONSE=$(curl -s -X POST "http://$API_HOST/api/execute" \
    -H "Content-Type: application/json" \
    -d "$REQUEST_BODY")

SESSION_ID=$(echo "$RESPONSE" | grep -o '"session_id":"[^"]*"' | cut -d'"' -f4)
STATUS=$(echo "$RESPONSE" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

if [ -z "$SESSION_ID" ]; then
    echo -e "${RED}Error: Failed to get session ID${NC}"
    echo "Response: $RESPONSE"
    exit 1
fi

echo -e "${GREEN}Execution started!${NC}"
echo "  Session ID: $SESSION_ID"
echo "  Status: $STATUS"
echo ""

# Monitor stream
echo -e "${YELLOW}=== Streaming logs (Ctrl+C to stop) ===${NC}"
echo ""

# Use curl to stream SSE events with unbuffered output
# The sed processes line by line and outputs immediately
exec 3< <(curl -s -N --no-buffer "http://$API_HOST/api/execute/$SESSION_ID/stream" 2>/dev/null)

EVENT_TYPE=""
HAD_ERROR=0
LAST_ERROR=""

print_stream_data() {
    local prefix="$1"
    local data="$2"
    local color="$3"
    local parse_content_json="${4:-0}"

    if [ "$JQ_AVAILABLE" -eq 1 ]; then
        if echo "$data" | jq -e . >/dev/null 2>&1; then
            echo -e "${color}${prefix}${NC}"
            if [ "$parse_content_json" -eq 1 ]; then
                echo "$data" | jq 'if (.content? and (.content | type == "string")) then .content |= (try fromjson catch .) else . end'
            else
                echo "$data" | jq .
            fi
        else
            echo -e "${color}${prefix} $data${NC}"
        fi
    else
        echo -e "${color}${prefix} $data${NC}"
    fi
}

while IFS= read -r line <&3; do
    # Parse SSE event format: "event: TYPE" followed by "data: JSON"
    if [[ "$line" == event:* ]]; then
        EVENT_TYPE="${line#event: }"
    elif [[ "$line" == data:* ]]; then
        DATA="${line#data: }"

        case "$EVENT_TYPE" in
            "stdout")
                print_stream_data "[stdout]" "$DATA" "$NC" 1
                ;;
            "stderr")
                print_stream_data "[stderr]" "$DATA" "$RED"
                ;;
            "error")
                print_stream_data "[ERROR]" "$DATA" "$RED"
                HAD_ERROR=1
                LAST_ERROR="$DATA"
                exec 3<&-
                break
                ;;
            "done")
                echo ""
                echo -e "${GREEN}=== Execution completed ===${NC}"
                exec 3<&-
                break
                ;;
            *)
                print_stream_data "[$EVENT_TYPE]" "$DATA" "$NC"
                ;;
        esac
    fi
done

if [ "$HAD_ERROR" -eq 1 ]; then
    echo ""
    echo -e "${RED}=== Execution failed ===${NC}"
    echo "  Session ID: $SESSION_ID"
    echo "  Error: $LAST_ERROR"
    exit 1
fi

# Check if files were created
echo ""
echo -e "${YELLOW}=== Files in working directory ===${NC}"
ls -la "$WORKING_DIR" 2>/dev/null || echo "(directory empty or not accessible)"

echo ""
echo -e "${GREEN}Test completed!${NC}"
