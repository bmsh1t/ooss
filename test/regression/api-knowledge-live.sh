#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-api-knowledge-live}"
PORT="${PORT:-8907}"
BASE_URL="http://127.0.0.1:${PORT}/osm/api"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
SETTINGS_TEMPLATE="${SETTINGS_TEMPLATE:-${ROOT_DIR}/public/presets/osm-settings.example.yaml}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
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

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "assertion failed for ${label}: expected '${haystack}' to contain '${needle}'" >&2
    exit 1
  fi
}

wait_for_server() {
  local health_url="http://127.0.0.1:${PORT}/health"
  for _ in $(seq 1 50); do
    if curl -sf "$health_url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "server did not become healthy: ${health_url}" >&2
  exit 1
}

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd curl
require_cmd jq
require_cmd grep

if [[ ! -f "$SETTINGS_TEMPLATE" ]]; then
  echo "settings template not found: $SETTINGS_TEMPLATE" >&2
  exit 1
fi

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR" "$WORKSPACES_DIR"
cp "$SETTINGS_TEMPLATE" "$BASE_DIR/osm-settings.yaml"
cat >>"$BASE_DIR/osm-settings.yaml" <<'EOF'

knowledge_vector:
  enabled: false
  auto_index_on_ingest: false
  auto_index_on_learn: false
EOF

KB_ROOT="$BASE_DIR/kb-source"
mkdir -p "$KB_ROOT/nested"
cat >"$KB_ROOT/notes.md" <<'EOF'
# JWT Bypass Notes

Use audience validation and token confusion checks when reviewing the login gateway.
EOF
cat >"$KB_ROOT/nested/playbook.txt" <<'EOF'
Admin panel exposure checklist:
- verify auth middleware
- review forgotten preview routes
EOF

WORKSPACE="kb-regression.example.com"
WORKSPACE_DIR="$WORKSPACES_DIR/$WORKSPACE"
mkdir -p "$WORKSPACE_DIR/ai-analysis"
cat >"$WORKSPACE_DIR/ai-analysis/unified-analysis-$WORKSPACE.md" <<'EOF'
Focus on login review and token confusion checks before opening a deeper manual workflow.
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  serve \
  --host 127.0.0.1 \
  --port "$PORT" \
  -A \
  --no-hot-reload \
  --no-event-receiver \
  --no-schedule \
  --no-queue-polling \
  >"$BASE_DIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_server

verified_vuln_body=$(jq -nc --arg ws "$WORKSPACE" '{
  workspace: $ws,
  vuln_info: "admin-panel-exposure",
  vuln_title: "Admin Panel Exposure",
  vuln_desc: "Preview admin panel is reachable without expected auth checks.",
  severity: "high",
  confidence: "certain",
  asset_type: "url",
  asset_value: "https://kb-regression.example.com/admin",
  vuln_status: "verified",
  source_run_uuid: "kb-regression-run-1"
}')
curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$verified_vuln_body" >/dev/null

ingest_body=$(jq -nc --arg path "$KB_ROOT" --arg ws "$WORKSPACE" '{path: $path, workspace: $ws, recursive: true}')
ingest_resp=$(curl -sf -X POST "$BASE_URL/knowledge/ingest" -H 'Content-Type: application/json' -d "$ingest_body")
ingest_documents=$(printf '%s' "$ingest_resp" | jq -er '.data.documents')
ingest_chunks=$(printf '%s' "$ingest_resp" | jq -er '.data.chunks')
ingest_failed=$(printf '%s' "$ingest_resp" | jq -er '.data.failed')

docs_workspace_resp=$(curl -sf "$BASE_URL/knowledge/documents?workspace=$WORKSPACE")
docs_workspace_count=$(printf '%s' "$docs_workspace_resp" | jq -er '.data | length')

search_notes_body=$(jq -nc --arg ws "$WORKSPACE" '{query: "token confusion checks", workspace: $ws, limit: 5}')
search_notes_resp=$(curl -sf -X POST "$BASE_URL/knowledge/search" -H 'Content-Type: application/json' -d "$search_notes_body")
search_notes_total=$(printf '%s' "$search_notes_resp" | jq -er '.total')
search_notes_content=$(printf '%s' "$search_notes_resp" | jq -er '.data[0].snippet')

learn_body=$(jq -nc --arg ws "$WORKSPACE" '{workspace: $ws, scope: "public", include_ai_analysis: true}')
learn_resp=$(curl -sf -X POST "$BASE_URL/knowledge/learn" -H 'Content-Type: application/json' -d "$learn_body")
learn_documents=$(printf '%s' "$learn_resp" | jq -er '.data.documents')
learn_chunks=$(printf '%s' "$learn_resp" | jq -er '.data.chunks')
learn_storage_workspace=$(printf '%s' "$learn_resp" | jq -er '.data.storage_workspace')
learn_ai_file_count=$(printf '%s' "$learn_resp" | jq -er '.data.ai_files_included | length')

docs_public_resp=$(curl -sf "$BASE_URL/knowledge/documents?workspace=public")
docs_public_count=$(printf '%s' "$docs_public_resp" | jq -er '.data | length')

search_verified_body=$(jq -nc --arg ws "$WORKSPACE" '{
  query: "Admin Panel Exposure",
  workspace: $ws,
  limit: 5,
  sample_types: ["verified"]
}')
search_verified_resp=$(curl -sf -X POST "$BASE_URL/knowledge/search" -H 'Content-Type: application/json' -d "$search_verified_body")
search_verified_total=$(printf '%s' "$search_verified_resp" | jq -er '.total')
search_verified_content=$(printf '%s' "$search_verified_resp" | jq -er '.data[0].snippet')

search_ai_body=$(jq -nc --arg ws "$WORKSPACE" '{
  query: "token confusion checks",
  workspace: $ws,
  limit: 5,
  sample_types: ["ai-analysis"]
}')
search_ai_resp=$(curl -sf -X POST "$BASE_URL/knowledge/search" -H 'Content-Type: application/json' -d "$search_ai_body")
search_ai_total=$(printf '%s' "$search_ai_resp" | jq -er '.total')
search_ai_content=$(printf '%s' "$search_ai_resp" | jq -er '.data[0].snippet')

assert_eq "$ingest_documents" "2" "knowledge ingest document count"
assert_eq "$ingest_failed" "0" "knowledge ingest failed count"
if (( ingest_chunks < 2 )); then
  echo "assertion failed for knowledge ingest chunks: expected >= 2, got '${ingest_chunks}'" >&2
  exit 1
fi
assert_eq "$docs_workspace_count" "2" "workspace knowledge document count"
if (( search_notes_total < 1 )); then
  echo "assertion failed for knowledge search total: expected >= 1, got '${search_notes_total}'" >&2
  exit 1
fi
assert_contains "$search_notes_content" "token confusion checks" "knowledge search content"
if (( learn_documents < 3 )); then
  echo "assertion failed for learned document count: expected >= 3, got '${learn_documents}'" >&2
  exit 1
fi
if (( learn_chunks < 3 )); then
  echo "assertion failed for learned chunk count: expected >= 3, got '${learn_chunks}'" >&2
  exit 1
fi
assert_eq "$learn_storage_workspace" "public" "learn storage workspace"
if (( learn_ai_file_count < 1 )); then
  echo "assertion failed for learned ai file count: expected >= 1, got '${learn_ai_file_count}'" >&2
  exit 1
fi
if (( docs_public_count < 3 )); then
  echo "assertion failed for public knowledge document count: expected >= 3, got '${docs_public_count}'" >&2
  exit 1
fi
if (( search_verified_total < 1 )); then
  echo "assertion failed for verified knowledge search total: expected >= 1, got '${search_verified_total}'" >&2
  exit 1
fi
assert_contains "$search_verified_content" "Admin Panel Exposure" "verified knowledge content"
if (( search_ai_total < 1 )); then
  echo "assertion failed for ai-analysis knowledge search total: expected >= 1, got '${search_ai_total}'" >&2
  exit 1
fi
assert_contains "$search_ai_content" "token confusion checks" "ai-analysis knowledge content"

printf 'ok live api regression: knowledge ingest, search, learn\n'
