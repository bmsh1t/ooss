#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-llm-responses-smoke}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/test/testdata/workflows/agent-and-llm}"
MOCK_SERVER_SCRIPT="${MOCK_SERVER_SCRIPT:-${ROOT_DIR}/test/regression/mock-responses-server.py}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
REQUEST_LOG="${REQUEST_LOG:-${BASE_DIR}/responses-requests.jsonl}"
SERVER_LOG="${SERVER_LOG:-${BASE_DIR}/mock-server.log}"
PORT="${PORT:-18991}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "assertion failed for ${label}: expected output to contain '${needle}'" >&2
    exit 1
  fi
}

assert_eq() {
  local actual="$1"
  local expected="$2"
  local label="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "assertion failed for ${label}: expected '${expected}', got '${actual}'" >&2
    exit 1
  fi
}

wait_for_url() {
  local url="$1"
  local label="$2"
  for _ in $(seq 1 50); do
    if curl -sf "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "${label} did not become healthy: ${url}" >&2
  exit 1
}

find_free_tcp_port() {
  python3 - "$1" <<'PY'
import socket
import sys

start = int(sys.argv[1])
port = start
while True:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        try:
            s.bind(("127.0.0.1", port))
        except OSError:
            port += 1
            continue
        print(port)
        break
PY
}

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd python3
require_cmd jq
require_cmd curl

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

if [[ ! -f "$MOCK_SERVER_SCRIPT" ]]; then
  echo "mock responses server not found: $MOCK_SERVER_SCRIPT" >&2
  exit 1
fi

PORT="$(find_free_tcp_port "$PORT")"
rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR" "$WORKSPACES_DIR"

cat >"$BASE_DIR/osm-settings.yaml" <<EOF
base_folder: "$BASE_DIR"
environments:
  workspaces: "$WORKSPACES_DIR"
  workflows: "$WORKFLOW_DIR"
llm_config:
  llm_providers:
    - provider: mock
      base_url: "http://127.0.0.1:$PORT/v1/chat/completions"
      auth_token: ""
      model: "gpt-5.4"
  max_retries: 1
  timeout: "30s"
server:
  enabled_auth_api: false
EOF

python3 "$MOCK_SERVER_SCRIPT" \
  --host 127.0.0.1 \
  --port "$PORT" \
  --request-log "$REQUEST_LOG" \
  >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

wait_for_url "http://127.0.0.1:${PORT}/health" "mock responses server"

COMMON_ARGS=(
  --base-folder "$BASE_DIR"
  --settings-file "$BASE_DIR/osm-settings.yaml"
  -F "$WORKFLOW_DIR"
)

env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" \
  "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" workflow validate test-llm-responses >/dev/null

env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" \
  "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" workflow validate test-llm-responses-alias >/dev/null

workflow_output=$(
  env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" \
    "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" run -m test-llm-responses -t example.com
)
assert_contains "$workflow_output" "Responses summary: Native workflow response" "responses workflow output"

alias_output=$(
  env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" \
    "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" run -m test-llm-responses-alias -t example.com
)
assert_contains "$alias_output" "Responses alias summary: Alias workflow response" "responses alias workflow output"

invoke_output=$(
  env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" OSM_LLM_API_MODE=responses \
    "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" func e 'llm_invoke("hello responses")'
)
assert_contains "$invoke_output" "Function env response" "llm_invoke responses output"

custom_output=$(
  env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" \
    "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" func e 'llm_invoke_custom("latest recon news", "{\"model\":\"gpt-5.4\",\"input\":\"{{message}}\",\"tools\":[{\"type\":\"web_search_preview\"}]}")'
)
assert_contains "$custom_output" "Function custom response" "llm_invoke_custom responses output"

conversations_output=$(
  env OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" OSM_LLM_API_MODE=responses \
    "$OSMEDEUS_BIN" "${COMMON_ARGS[@]}" func e 'llm_conversations("system:Be brief", "user:Analyze example.com")'
)
assert_contains "$conversations_output" "Function conversations response" "llm_conversations responses output"

request_count=$(jq -s 'length' "$REQUEST_LOG")
responses_path_count=$(jq -s '[.[] | select(.path == "/v1/responses")] | length' "$REQUEST_LOG")
tool_request_count=$(jq -s '[.[] | select((.body.tools // []) | length > 0)] | length' "$REQUEST_LOG")
conversation_count=$(jq -s '[.[] | select((.body.input | type) == "array" and (.body.input | length) == 2)] | length' "$REQUEST_LOG")

assert_eq "$request_count" "5" "request count"
assert_eq "$responses_path_count" "5" "responses path count"
assert_eq "$tool_request_count" "3" "tool request count"
assert_eq "$conversation_count" "1" "conversation request count"

echo "ok llm responses smoke: workflow/api_mode, workflow/use_responses_api, llm_invoke, llm_invoke_custom, llm_conversations"
