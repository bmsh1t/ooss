#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-ai-semantic-vector-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/ai-semantic}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
EMBED_PORT="${EMBED_PORT:-8911}"
TARGET="${TARGET:-example.com}"
KNOWLEDGE_WORKSPACE="${KNOWLEDGE_WORKSPACE:-example.com}"
SHARED_WORKSPACE="${SHARED_WORKSPACE:-shared-web}"
GLOBAL_WORKSPACE="${GLOBAL_WORKSPACE:-global}"
MOCK_PROVIDER="${MOCK_PROVIDER:-mock-openai}"
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

dump_logs() {
  for file in \
    "$BASE_DIR/mock-embeddings.log" \
    "$BASE_DIR/kb-ingest-workspace.log" \
    "$BASE_DIR/kb-ingest-shared.log" \
    "$BASE_DIR/kb-ingest-global.log" \
    "$BASE_DIR/validate.log" \
    "$BASE_DIR/run.log"; do
    if [[ -f "$file" ]]; then
      echo "---- $(basename "$file") ----" >&2
      tail -n 120 "$file" >&2 || true
    fi
  done
}

cleanup() {
  if [[ -n "${EMBED_PID:-}" ]] && kill -0 "$EMBED_PID" >/dev/null 2>&1; then
    kill "$EMBED_PID" >/dev/null 2>&1 || true
    wait "$EMBED_PID" >/dev/null 2>&1 || true
  fi
}
trap 'rc=$?; cleanup; if [[ $rc -ne 0 ]]; then dump_logs; fi' EXIT

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
  auto_index_on_learn: false
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
mkdir -p "$KB_ROOT/workspace" "$KB_ROOT/shared" "$KB_ROOT/global"

cat >"$KB_ROOT/workspace/auth-playbook.md" <<'EOF'
# Workspace Auth Playbook

Token confusion admin panel preview route is the primary investigation path for this target.
Audit JWT audience validation, session replay paths, admin panel boundaries, and preview middleware bypasses.
EOF

cat >"$KB_ROOT/shared/operator-notes.txt" <<'EOF'
Shared review notes:
- token confusion admin panel preview route often pairs with weak auth middleware
- preview routes should be checked for access control drift
EOF

cat >"$KB_ROOT/global/exploitation-guide.txt" <<'EOF'
Global exploitation guide:
- focus on token confusion admin panel preview route and admin console exposure
- confirm whether GraphQL and login surfaces share the same auth boundary
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  kb ingest --path "$KB_ROOT/workspace" -w "$KNOWLEDGE_WORKSPACE" --recursive \
  >"$BASE_DIR/kb-ingest-workspace.log" 2>&1

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  kb ingest --path "$KB_ROOT/shared" -w "$SHARED_WORKSPACE" --recursive \
  >"$BASE_DIR/kb-ingest-shared.log" 2>&1

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  kb ingest --path "$KB_ROOT/global" -w "$GLOBAL_WORKSPACE" --recursive \
  >"$BASE_DIR/kb-ingest-global.log" 2>&1

vector_stats_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb vector stats)
vector_stats_documents=$(printf '%s' "$vector_stats_json" | jq -er '.documents')
vector_stats_chunks=$(printf '%s' "$vector_stats_json" | jq -er '.chunks')
vector_stats_embeddings=$(printf '%s' "$vector_stats_json" | jq -er '.embeddings')
vector_stats_workspaces=$(printf '%s' "$vector_stats_json" | jq -er '.workspaces | length')
vector_stats_model=$(printf '%s' "$vector_stats_json" | jq -er '.models[0]')
direct_vector_search_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb vector search -w "$KNOWLEDGE_WORKSPACE" --query "token confusion admin panel preview route" --limit 3)
direct_vector_total=$(printf '%s' "$direct_vector_search_json" | jq -er 'length')
direct_keyword_search_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb search -w "$KNOWLEDGE_WORKSPACE" --query "token confusion admin panel preview route" --limit 3)
direct_keyword_total=$(printf '%s' "$direct_keyword_search_json" | jq -er 'length')

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate ai-semantic-vector-smoke-flow \
  >"$BASE_DIR/validate.log" 2>&1

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  run -f ai-semantic-vector-smoke-flow -t "$TARGET" \
  >"$BASE_DIR/run.log" 2>&1

WORKSPACE_DIR=$(find "$WORKSPACES_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)
if [[ -z "${WORKSPACE_DIR:-}" ]]; then
  echo "no workspace directory created under $WORKSPACES_DIR" >&2
  exit 1
fi
WORKSPACE=$(basename "$WORKSPACE_DIR")
AI_DIR="$WORKSPACE_DIR/ai-analysis"

SEMANTIC_RESULTS="$AI_DIR/semantic-search-results-$WORKSPACE.json"
SEMANTIC_VECTOR="$AI_DIR/vector-search-results-$WORKSPACE.json"
SEMANTIC_VECTOR_KB="$AI_DIR/vector-kb-search-results-$WORKSPACE.json"
SEMANTIC_KB="$AI_DIR/knowledge-search-results-$WORKSPACE.json"
SEMANTIC_HIGHLIGHTS="$AI_DIR/semantic-highlights-$WORKSPACE.json"
SEMANTIC_QUERY="$AI_DIR/semantic-index/resolved-search-query.txt"
SEMANTIC_LOG_DIR="$AI_DIR/semantic-index/logs"

HYBRID_RESULTS="$AI_DIR/hybrid-search-results-$WORKSPACE.json"
HYBRID_VECTOR="$AI_DIR/hybrid-vector-search-results-$WORKSPACE.json"
HYBRID_VECTOR_KB="$AI_DIR/hybrid-vector-kb-results-$WORKSPACE.json"
HYBRID_KB="$AI_DIR/hybrid-knowledge-search-results-$WORKSPACE.json"
HYBRID_HIGHLIGHTS="$AI_DIR/hybrid-search-highlights-$WORKSPACE.json"
HYBRID_QUERY="$AI_DIR/hybrid-semantic-index/resolved-search-query.txt"
HYBRID_LOG_DIR="$AI_DIR/hybrid-semantic-index/logs"

for required_file in \
  "$SEMANTIC_RESULTS" \
  "$SEMANTIC_VECTOR" \
  "$SEMANTIC_VECTOR_KB" \
  "$SEMANTIC_KB" \
  "$SEMANTIC_HIGHLIGHTS" \
  "$SEMANTIC_QUERY" \
  "$HYBRID_RESULTS" \
  "$HYBRID_VECTOR" \
  "$HYBRID_VECTOR_KB" \
  "$HYBRID_KB" \
  "$HYBRID_HIGHLIGHTS" \
  "$HYBRID_QUERY"; do
  [[ -f "$required_file" ]] || {
    echo "missing expected artifact: $required_file" >&2
    exit 1
  }
done

semantic_total=$(jq -er '.total_results // ([.results[]?] | length) // 0' "$SEMANTIC_RESULTS")
semantic_provider=$(jq -er '.provider' "$SEMANTIC_VECTOR")
semantic_ready=$(jq -er '.ready' "$SEMANTIC_VECTOR")
semantic_status=$(jq -er '.semantic_status' "$SEMANTIC_VECTOR")
semantic_reason=$(jq -er '.reason // ""' "$SEMANTIC_VECTOR")
semantic_vector_total=$(jq -er '.total_results // 0' "$SEMANTIC_VECTOR")
semantic_vector_kb_hits=$(jq -er 'length' "$SEMANTIC_VECTOR_KB")
semantic_kb_hits=$(jq -er 'length' "$SEMANTIC_KB")
semantic_query=$(tr '\n' ' ' < "$SEMANTIC_QUERY")
semantic_knowledge_query=$(tr '\n' ' ' < "$AI_DIR/semantic-index/knowledge-search-query.txt")
semantic_highlights=$(cat "$SEMANTIC_HIGHLIGHTS")
semantic_index=$(cat "$AI_DIR/semantic-index/knowledge-index.txt")
semantic_export_log=$(cat "$SEMANTIC_LOG_DIR/semantic-kb-export-primary.log")

assert_ge "$semantic_total" 1 "semantic total results"
assert_eq "$semantic_provider" "$MOCK_PROVIDER" "semantic vector provider"
assert_eq "$semantic_ready" "true" "semantic vector ready"
assert_eq "$semantic_status" "ready" "semantic vector status"
assert_contains "$semantic_reason" "ready" "semantic vector reason"
assert_ge "$semantic_vector_total" 1 "semantic vector total results"
assert_ge "$semantic_vector_kb_hits" 1 "semantic vector kb hits"
assert_ge "$semantic_kb_hits" 1 "semantic merged kb hits"
assert_contains "$semantic_query" "token confusion" "semantic resolved query"
assert_contains "$semantic_knowledge_query" "token confusion admin panel preview route" "semantic knowledge query"
assert_contains "$semantic_index" "Token confusion admin panel preview route is the primary investigation path" "semantic knowledge index"
assert_contains "$semantic_index" "workspace=shared-web" "semantic shared knowledge export"
assert_contains "$semantic_index" "workspace=global" "semantic global knowledge export"
assert_contains "$semantic_highlights" "critical_findings" "semantic highlights shape"
assert_contains "$semantic_export_log" "example.com" "semantic primary export log"

hybrid_total=$(jq -er '.total_results // 0' "$HYBRID_RESULTS")
hybrid_vector_total=$(jq -er '.total_results // 0' "$HYBRID_VECTOR")
hybrid_vector_recall=$(jq -er '.vector_recall.count // 0' "$HYBRID_RESULTS")
hybrid_vector_kb_hits=$(jq -er 'length' "$HYBRID_VECTOR_KB")
hybrid_kb_hits=$(jq -er 'length' "$HYBRID_KB")
hybrid_provider=$(jq -er '.provider' "$HYBRID_VECTOR")
hybrid_ready=$(jq -er '.ready' "$HYBRID_VECTOR")
hybrid_status=$(jq -er '.semantic_status' "$HYBRID_VECTOR")
hybrid_reason=$(jq -er '.reason // ""' "$HYBRID_VECTOR")
hybrid_query=$(tr '\n' ' ' < "$HYBRID_QUERY")
hybrid_knowledge_query=$(tr '\n' ' ' < "$AI_DIR/hybrid-semantic-index/knowledge-search-query.txt")
hybrid_highlights=$(cat "$HYBRID_HIGHLIGHTS")
hybrid_export_log=$(cat "$HYBRID_LOG_DIR/hybrid-kb-export-primary.log")
hybrid_scan_hits=$(jq -er '.scan_data.count // 0' "$HYBRID_RESULTS")

assert_ge "$hybrid_total" 1 "hybrid total results"
assert_eq "$hybrid_provider" "$MOCK_PROVIDER" "hybrid vector provider"
assert_eq "$hybrid_ready" "true" "hybrid vector ready"
assert_eq "$hybrid_status" "ready" "hybrid vector status"
assert_contains "$hybrid_reason" "ready" "hybrid vector reason"
assert_ge "$hybrid_vector_total" 1 "hybrid vector total results"
assert_ge "$hybrid_vector_recall" 1 "hybrid vector recall count"
assert_ge "$hybrid_vector_kb_hits" 1 "hybrid vector kb hits"
assert_ge "$hybrid_kb_hits" 1 "hybrid merged kb hits"
assert_ge "$hybrid_scan_hits" 1 "hybrid scan fallback count"
assert_contains "$hybrid_query" "token confusion" "hybrid resolved query"
assert_contains "$hybrid_knowledge_query" "token confusion admin panel preview route" "hybrid knowledge query"
assert_contains "$hybrid_highlights" "relevant_knowledge" "hybrid highlights shape"
assert_contains "$hybrid_export_log" "example.com" "hybrid primary export log"

assert_eq "$vector_stats_documents" "3" "vector stats documents"
assert_eq "$vector_stats_chunks" "3" "vector stats chunks"
assert_eq "$vector_stats_embeddings" "3" "vector stats embeddings"
assert_eq "$vector_stats_workspaces" "3" "vector stats workspaces"
assert_eq "$vector_stats_model" "${MOCK_PROVIDER}:${EMBED_MODEL}" "vector stats model"
assert_ge "$direct_vector_total" 1 "direct vector json search hits"
assert_ge "$direct_keyword_total" 1 "direct keyword json search hits"

echo "ok ai semantic vector smoke regression: provider-enabled semantic and hybrid workflow execution with live KB hits"
