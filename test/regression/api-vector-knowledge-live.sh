#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-api-vector-live}"
PORT="${PORT:-8909}"
EMBED_PORT="${EMBED_PORT:-8910}"
BASE_URL="http://127.0.0.1:${PORT}/osm/api"
NEG_BASE_DIR="${NEG_BASE_DIR:-${BASE_DIR}-warn}"
NEG_PORT="${NEG_PORT:-8912}"
NEG_BASE_URL="http://127.0.0.1:${NEG_PORT}/osm/api"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
NEG_WORKSPACES_DIR="${NEG_WORKSPACES_DIR:-${NEG_BASE_DIR}/workspaces}"
MOCK_PROVIDER="${MOCK_PROVIDER:-mock-openai}"
NEG_PROVIDER="${NEG_PROVIDER:-missing-openai}"
EMBED_MODEL="${EMBED_MODEL:-test-embedding-3-small}"
MOCK_SERVER_SCRIPT="${MOCK_SERVER_SCRIPT:-${ROOT_DIR}/test/regression/mock-embedding-server.py}"

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

assert_ge() {
  local actual="$1"
  local expected="$2"
  local label="$3"
  if (( actual < expected )); then
    echo "assertion failed for ${label}: expected >= ${expected}, got '${actual}'" >&2
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
  local start_port="$1"
  local port="$start_port"
  while lsof -iTCP:"$port" -sTCP:LISTEN -n -P >/dev/null 2>&1; do
    port=$((port + 1))
  done
  printf '%s\n' "$port"
}

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${NEG_SERVER_PID:-}" ]] && kill -0 "$NEG_SERVER_PID" >/dev/null 2>&1; then
    kill "$NEG_SERVER_PID" >/dev/null 2>&1 || true
    wait "$NEG_SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${EMBED_PID:-}" ]] && kill -0 "$EMBED_PID" >/dev/null 2>&1; then
    kill "$EMBED_PID" >/dev/null 2>&1 || true
    wait "$EMBED_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd curl
require_cmd jq
require_cmd lsof
require_cmd python3

if [[ ! -f "$MOCK_SERVER_SCRIPT" ]]; then
  echo "mock embeddings server not found: $MOCK_SERVER_SCRIPT" >&2
  exit 1
fi

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

EMBED_PORT="$(find_free_tcp_port "$EMBED_PORT")"

rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR" "$WORKSPACES_DIR"

cat >"$BASE_DIR/osm-settings.yaml" <<EOF
base_folder: "$BASE_DIR"
environments:
  workspaces: "$WORKSPACES_DIR"
  workflows: "$WORKFLOW_DIR"
database:
  db_engine: sqlite
  db_path: "{{base_folder}}/database-osm.sqlite"
knowledge_vector:
  enabled: true
  db_path: "{{base_folder}}/knowledge/vector-kb.sqlite"
  default_provider: "$MOCK_PROVIDER"
  default_model: "$EMBED_MODEL"
  auto_index_on_ingest: true
  auto_index_on_learn: true
  top_k: 10
  hybrid_weight: 0.7
  keyword_weight: 0.3
  batch_size: 8
  max_indexing_chunks: 200
llm_config:
  llm_providers:
    - provider: "$MOCK_PROVIDER"
      base_url: "http://127.0.0.1:$EMBED_PORT/embeddings"
      auth_token: ""
      model: "$EMBED_MODEL"
  max_retries: 1
  timeout: 5s
server:
  enabled_auth_api: false
EOF

python3 "$MOCK_SERVER_SCRIPT" --host 127.0.0.1 --port "$EMBED_PORT" --model "$EMBED_MODEL" \
  >"$BASE_DIR/mock-embeddings.log" 2>&1 &
EMBED_PID=$!
wait_for_url "http://127.0.0.1:${EMBED_PORT}/health" "mock embeddings server"

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

WORKSPACE="kb-vector-regression.example.com"
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

wait_for_url "http://127.0.0.1:${PORT}/health" "osmedeus server"

initial_stats_resp=$(curl -sf "$BASE_URL/knowledge/vector/stats")
initial_documents=$(printf '%s' "$initial_stats_resp" | jq -er '.data.documents')
initial_chunks=$(printf '%s' "$initial_stats_resp" | jq -er '.data.chunks')
initial_embeddings=$(printf '%s' "$initial_stats_resp" | jq -er '.data.embeddings')

verified_vuln_body=$(jq -nc --arg ws "$WORKSPACE" '{
  workspace: $ws,
  vuln_info: "admin-panel-exposure",
  vuln_title: "Admin Panel Exposure",
  vuln_desc: "Preview admin panel is reachable without expected auth checks.",
  severity: "high",
  confidence: "certain",
  asset_type: "url",
  asset_value: "https://kb-vector-regression.example.com/admin",
  vuln_status: "verified",
  source_run_uuid: "kb-vector-regression-run-1"
}')
curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$verified_vuln_body" >/dev/null

ingest_body=$(jq -nc --arg path "$KB_ROOT" --arg ws "$WORKSPACE" '{path: $path, workspace: $ws, recursive: true}')
ingest_resp=$(curl -sf -X POST "$BASE_URL/knowledge/ingest" -H 'Content-Type: application/json' -d "$ingest_body")
ingest_documents=$(printf '%s' "$ingest_resp" | jq -er '.data.documents')
ingest_chunks=$(printf '%s' "$ingest_resp" | jq -er '.data.chunks')
ingest_failed=$(printf '%s' "$ingest_resp" | jq -er '.data.failed')

stats_after_ingest_resp=$(curl -sf "$BASE_URL/knowledge/vector/stats")
stats_after_ingest_documents=$(printf '%s' "$stats_after_ingest_resp" | jq -er '.data.documents')
stats_after_ingest_chunks=$(printf '%s' "$stats_after_ingest_resp" | jq -er '.data.chunks')
stats_after_ingest_embeddings=$(printf '%s' "$stats_after_ingest_resp" | jq -er '.data.embeddings')
stats_after_ingest_workspaces=$(printf '%s' "$stats_after_ingest_resp" | jq -er '.data.workspaces | length')
stats_after_ingest_models=$(printf '%s' "$stats_after_ingest_resp" | jq -er '.data.models[0]')
doctor_after_ingest_resp=$(curl -sf "$BASE_URL/knowledge/vector/doctor?workspace=${WORKSPACE}")
doctor_after_ingest_status=$(printf '%s' "$doctor_after_ingest_resp" | jq -er '.data.semantic_status')
doctor_after_ingest_ready=$(printf '%s' "$doctor_after_ingest_resp" | jq -er '.data.semantic_search_ready')
doctor_after_ingest_selected_embeddings=$(printf '%s' "$doctor_after_ingest_resp" | jq -er '.data.selected_embeddings')
doctor_after_ingest_missing_documents=$(printf '%s' "$doctor_after_ingest_resp" | jq -er '.data.missing_documents')

reindex_resp=$(curl -sf -X POST "$BASE_URL/knowledge/vector/index" -H 'Content-Type: application/json' -d "$(jq -nc --arg ws "$WORKSPACE" '{workspace: $ws}')")
reindex_documents_seen=$(printf '%s' "$reindex_resp" | jq -er '.data.documents_seen')
reindex_documents_indexed=$(printf '%s' "$reindex_resp" | jq -er '.data.documents_indexed')
reindex_documents_skipped=$(printf '%s' "$reindex_resp" | jq -er '.data.documents_skipped')
reindex_chunks_embedded=$(printf '%s' "$reindex_resp" | jq -er '.data.chunks_embedded')

vector_search_workspace_body=$(jq -nc --arg ws "$WORKSPACE" '{
  query: "token confusion checks",
  workspace: $ws,
  limit: 5
}')
vector_search_workspace_resp=$(curl -sf -X POST "$BASE_URL/knowledge/vector/search" -H 'Content-Type: application/json' -d "$vector_search_workspace_body")
vector_search_workspace_total=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.total')
vector_search_workspace_hit_workspace=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.data[0].workspace')
vector_search_workspace_provider=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.data[0].provider')
vector_search_workspace_model=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.data[0].model')
vector_search_workspace_type=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.data[0].type')
vector_search_workspace_content=$(printf '%s' "$vector_search_workspace_resp" | jq -er '.data[0].content')

learn_body=$(jq -nc --arg ws "$WORKSPACE" '{workspace: $ws, scope: "public", include_ai_analysis: true}')
learn_resp=$(curl -sf -X POST "$BASE_URL/knowledge/learn" -H 'Content-Type: application/json' -d "$learn_body")
learn_documents=$(printf '%s' "$learn_resp" | jq -er '.data.documents')
learn_chunks=$(printf '%s' "$learn_resp" | jq -er '.data.chunks')
learn_storage_workspace=$(printf '%s' "$learn_resp" | jq -er '.data.storage_workspace')
learn_ai_file_count=$(printf '%s' "$learn_resp" | jq -er '.data.ai_files_included | length')

stats_after_learn_resp=$(curl -sf "$BASE_URL/knowledge/vector/stats")
stats_after_learn_documents=$(printf '%s' "$stats_after_learn_resp" | jq -er '.data.documents')
stats_after_learn_chunks=$(printf '%s' "$stats_after_learn_resp" | jq -er '.data.chunks')
stats_after_learn_embeddings=$(printf '%s' "$stats_after_learn_resp" | jq -er '.data.embeddings')
stats_after_learn_workspaces=$(printf '%s' "$stats_after_learn_resp" | jq -er '.data.workspaces | length')
stats_after_learn_public_present=$(printf '%s' "$stats_after_learn_resp" | jq -er '.data.workspaces | index("public") != null')
doctor_after_learn_resp=$(curl -sf "$BASE_URL/knowledge/vector/doctor?workspace=public")
doctor_after_learn_status=$(printf '%s' "$doctor_after_learn_resp" | jq -er '.data.semantic_status')
doctor_after_learn_ready=$(printf '%s' "$doctor_after_learn_resp" | jq -er '.data.semantic_search_ready')
doctor_after_learn_selected_embeddings=$(printf '%s' "$doctor_after_learn_resp" | jq -er '.data.selected_embeddings')
doctor_after_learn_missing_documents=$(printf '%s' "$doctor_after_learn_resp" | jq -er '.data.missing_documents')

vector_search_verified_body=$(jq -nc --arg ws "$WORKSPACE" '{
  query: "Admin Panel Exposure",
  workspace: $ws,
  limit: 5,
  sample_types: ["verified"]
}')
vector_search_verified_resp=$(curl -sf -X POST "$BASE_URL/knowledge/vector/search" -H 'Content-Type: application/json' -d "$vector_search_verified_body")
vector_search_verified_total=$(printf '%s' "$vector_search_verified_resp" | jq -er '.total')
vector_search_verified_workspace=$(printf '%s' "$vector_search_verified_resp" | jq -er '.data[0].workspace')
vector_search_verified_sample_type=$(printf '%s' "$vector_search_verified_resp" | jq -er '.data[0].metadata.sample_type')
vector_search_verified_content=$(printf '%s' "$vector_search_verified_resp" | jq -er '.data[0].content')

vector_search_ai_body=$(jq -nc --arg ws "$WORKSPACE" '{
  query: "token confusion checks",
  workspace: $ws,
  limit: 5,
  sample_types: ["ai-analysis"]
}')
vector_search_ai_resp=$(curl -sf -X POST "$BASE_URL/knowledge/vector/search" -H 'Content-Type: application/json' -d "$vector_search_ai_body")
vector_search_ai_total=$(printf '%s' "$vector_search_ai_resp" | jq -er '.total')
vector_search_ai_workspace=$(printf '%s' "$vector_search_ai_resp" | jq -er '.data[0].workspace')
vector_search_ai_sample_type=$(printf '%s' "$vector_search_ai_resp" | jq -er '.data[0].metadata.sample_type')
vector_search_ai_content=$(printf '%s' "$vector_search_ai_resp" | jq -er '.data[0].content')

assert_eq "$initial_documents" "0" "initial vector document count"
assert_eq "$initial_chunks" "0" "initial vector chunk count"
assert_eq "$initial_embeddings" "0" "initial vector embedding count"
assert_eq "$ingest_documents" "2" "vector knowledge ingest document count"
assert_eq "$ingest_failed" "0" "vector knowledge ingest failed count"
assert_ge "$ingest_chunks" "2" "vector knowledge ingest chunks"
assert_eq "$stats_after_ingest_documents" "2" "vector stats documents after ingest"
assert_ge "$stats_after_ingest_chunks" "2" "vector stats chunks after ingest"
assert_ge "$stats_after_ingest_embeddings" "2" "vector stats embeddings after ingest"
assert_eq "$stats_after_ingest_workspaces" "1" "vector stats workspaces after ingest"
assert_eq "$stats_after_ingest_models" "${MOCK_PROVIDER}:${EMBED_MODEL}" "vector stats model registry"
assert_eq "$doctor_after_ingest_status" "ready" "doctor status after ingest"
assert_eq "$doctor_after_ingest_ready" "true" "doctor ready after ingest"
assert_ge "$doctor_after_ingest_selected_embeddings" "2" "doctor selected embeddings after ingest"
assert_eq "$doctor_after_ingest_missing_documents" "0" "doctor missing documents after ingest"
assert_eq "$reindex_documents_seen" "2" "vector reindex documents seen"
assert_eq "$reindex_documents_indexed" "0" "vector reindex documents indexed"
assert_eq "$reindex_documents_skipped" "2" "vector reindex documents skipped"
assert_eq "$reindex_chunks_embedded" "0" "vector reindex chunks embedded"
assert_ge "$vector_search_workspace_total" "1" "workspace vector search total"
assert_eq "$vector_search_workspace_hit_workspace" "$WORKSPACE" "workspace vector search layer"
assert_eq "$vector_search_workspace_provider" "$MOCK_PROVIDER" "workspace vector search provider"
assert_eq "$vector_search_workspace_model" "$EMBED_MODEL" "workspace vector search model"
assert_eq "$vector_search_workspace_type" "vector_kb" "workspace vector search type"
assert_contains "$vector_search_workspace_content" "token confusion checks" "workspace vector search content"
assert_ge "$learn_documents" "3" "vector learn documents"
assert_ge "$learn_chunks" "3" "vector learn chunks"
assert_eq "$learn_storage_workspace" "public" "vector learn storage workspace"
assert_ge "$learn_ai_file_count" "1" "vector learn ai files"
assert_ge "$stats_after_learn_documents" "5" "vector stats documents after learn"
assert_ge "$stats_after_learn_chunks" "5" "vector stats chunks after learn"
assert_ge "$stats_after_learn_embeddings" "5" "vector stats embeddings after learn"
assert_eq "$stats_after_learn_workspaces" "2" "vector stats workspaces after learn"
assert_eq "$stats_after_learn_public_present" "true" "vector stats public workspace presence"
assert_eq "$doctor_after_learn_status" "ready" "doctor status after learn"
assert_eq "$doctor_after_learn_ready" "true" "doctor ready after learn"
assert_ge "$doctor_after_learn_selected_embeddings" "3" "doctor selected embeddings after learn"
assert_eq "$doctor_after_learn_missing_documents" "0" "doctor missing documents after learn"
assert_ge "$vector_search_verified_total" "1" "verified vector search total"
assert_eq "$vector_search_verified_workspace" "public" "verified vector search fallback workspace"
assert_eq "$vector_search_verified_sample_type" "verified" "verified vector search sample type"
assert_contains "$vector_search_verified_content" "Admin Panel Exposure" "verified vector search content"
assert_ge "$vector_search_ai_total" "1" "ai-analysis vector search total"
assert_eq "$vector_search_ai_workspace" "public" "ai-analysis vector search fallback workspace"
assert_eq "$vector_search_ai_sample_type" "ai-analysis" "ai-analysis vector search sample type"
assert_contains "$vector_search_ai_content" "token confusion checks" "ai-analysis vector search content"

rm -rf "$NEG_BASE_DIR"
mkdir -p "$NEG_BASE_DIR" "$NEG_WORKSPACES_DIR"

cat >"$NEG_BASE_DIR/osm-settings.yaml" <<EOF
base_folder: "$NEG_BASE_DIR"
environments:
  workspaces: "$NEG_WORKSPACES_DIR"
  workflows: "$WORKFLOW_DIR"
database:
  db_engine: sqlite
  db_path: "{{base_folder}}/database-osm.sqlite"
knowledge_vector:
  enabled: true
  db_path: "{{base_folder}}/knowledge/vector-kb.sqlite"
  default_provider: "$NEG_PROVIDER"
  default_model: "$EMBED_MODEL"
  auto_index_on_ingest: true
  auto_index_on_learn: true
  top_k: 10
  hybrid_weight: 0.7
  keyword_weight: 0.3
  batch_size: 8
  max_indexing_chunks: 200
llm_config:
  llm_providers:
    - provider: "$MOCK_PROVIDER"
      base_url: "http://127.0.0.1:$EMBED_PORT/embeddings"
      auth_token: ""
      model: "$EMBED_MODEL"
  max_retries: 1
  timeout: 5s
server:
  enabled_auth_api: false
EOF

NEG_KB_ROOT="$NEG_BASE_DIR/kb-source"
mkdir -p "$NEG_KB_ROOT" "$NEG_WORKSPACES_DIR/$WORKSPACE/ai-analysis"
cat >"$NEG_KB_ROOT/notes.md" <<'EOF'
# Preview Auth Route Notes

Document the preview auth route and token confusion investigation steps.
EOF
cat >"$NEG_WORKSPACES_DIR/$WORKSPACE/ai-analysis/unified-analysis-$WORKSPACE.md" <<'EOF'
Revisit preview auth route exposure and token confusion decisions before deeper follow-up.
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$NEG_WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$NEG_BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  serve \
  --host 127.0.0.1 \
  --port "$NEG_PORT" \
  -A \
  --no-hot-reload \
  --no-event-receiver \
  --no-schedule \
  --no-queue-polling \
  >"$NEG_BASE_DIR/server.log" 2>&1 &
NEG_SERVER_PID=$!

wait_for_url "http://127.0.0.1:${NEG_PORT}/health" "negative osmedeus server"

neg_verified_vuln_body=$(jq -nc --arg ws "$WORKSPACE" '{
  workspace: $ws,
  vuln_info: "preview-auth-exposure",
  vuln_title: "Preview Auth Exposure",
  vuln_desc: "Preview route is reachable without expected auth checks.",
  severity: "medium",
  confidence: "firm",
  asset_type: "url",
  asset_value: "https://kb-vector-regression.example.com/preview",
  vuln_status: "verified",
  source_run_uuid: "kb-vector-regression-run-neg-1"
}')
curl -sf -X POST "$NEG_BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$neg_verified_vuln_body" >/dev/null

neg_ingest_body=$(jq -nc --arg path "$NEG_KB_ROOT" --arg ws "$WORKSPACE" '{path: $path, workspace: $ws, recursive: true}')
neg_ingest_resp=$(curl -sf -X POST "$NEG_BASE_URL/knowledge/ingest" -H 'Content-Type: application/json' -d "$neg_ingest_body")
neg_ingest_documents=$(printf '%s' "$neg_ingest_resp" | jq -er '.data.documents')
neg_ingest_vector_indexed=$(printf '%s' "$neg_ingest_resp" | jq -r '.data.vector_indexed')
neg_ingest_vector_error=$(printf '%s' "$neg_ingest_resp" | jq -er '.data.vector_error')
neg_ingest_warning=$(printf '%s' "$neg_ingest_resp" | jq -er '.warning')

neg_doctor_resp=$(curl -sf "$NEG_BASE_URL/knowledge/vector/doctor?workspace=${WORKSPACE}")
neg_doctor_status=$(printf '%s' "$neg_doctor_resp" | jq -er '.data.semantic_status')
neg_doctor_ready=$(printf '%s' "$neg_doctor_resp" | jq -r '.data.semantic_search_ready')
neg_doctor_missing_documents=$(printf '%s' "$neg_doctor_resp" | jq -er '.data.missing_documents')

neg_learn_body=$(jq -nc --arg ws "$WORKSPACE" '{workspace: $ws, scope: "public", include_ai_analysis: true}')
neg_learn_resp=$(curl -sf -X POST "$NEG_BASE_URL/knowledge/learn" -H 'Content-Type: application/json' -d "$neg_learn_body")
neg_learn_documents=$(printf '%s' "$neg_learn_resp" | jq -er '.data.documents')
neg_learn_storage_workspace=$(printf '%s' "$neg_learn_resp" | jq -er '.data.storage_workspace')
neg_learn_vector_indexed=$(printf '%s' "$neg_learn_resp" | jq -r '.data.vector_indexed')
neg_learn_vector_error=$(printf '%s' "$neg_learn_resp" | jq -er '.data.vector_error')
neg_learn_warning=$(printf '%s' "$neg_learn_resp" | jq -er '.warning')

assert_eq "$neg_ingest_documents" "1" "negative ingest document count"
assert_eq "$neg_ingest_vector_indexed" "false" "negative ingest vector indexed flag"
assert_contains "$neg_ingest_vector_error" "not configured" "negative ingest vector error"
assert_contains "$neg_ingest_warning" "Vector auto-index failed" "negative ingest warning"
assert_eq "$neg_doctor_status" "provider_not_available" "negative doctor status"
assert_eq "$neg_doctor_ready" "false" "negative doctor ready"
assert_ge "$neg_doctor_missing_documents" "1" "negative doctor missing documents"
assert_ge "$neg_learn_documents" "2" "negative learn document count"
assert_eq "$neg_learn_storage_workspace" "public" "negative learn storage workspace"
assert_eq "$neg_learn_vector_indexed" "false" "negative learn vector indexed flag"
assert_contains "$neg_learn_vector_error" "not configured" "negative learn vector error"
assert_contains "$neg_learn_warning" "Vector auto-index failed" "negative learn warning"

printf 'ok live api regression: vector knowledge ingest, doctor, auto-index, search, learn, and warning-mode API success\n'
