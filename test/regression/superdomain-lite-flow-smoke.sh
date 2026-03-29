#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-superdomain-lite-flow-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
EMBED_PORT="${EMBED_PORT:-8913}"
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
      tail -n 160 "$file" >&2 || true
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
mkdir -p "$KB_ROOT/workspace" "$KB_ROOT/shared" "$KB_ROOT/global"

cat >"$KB_ROOT/workspace/auth-playbook.md" <<'EOF'
# Workspace Auth Playbook

Token confusion admin panel preview route is the primary investigation path for this target.
Audit JWT audience validation, preview middleware bypasses, and admin console trust boundaries.
EOF

cat >"$KB_ROOT/shared/operator-notes.txt" <<'EOF'
Shared review notes:
- token confusion admin panel preview route often pairs with weak auth middleware
- admin preview routes should be checked for access-control drift
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

WORKSPACE_SEED_DIR="$WORKSPACES_DIR/$TARGET"
mkdir -p \
  "$WORKSPACE_SEED_DIR/subdomain" \
  "$WORKSPACE_SEED_DIR/probing" \
  "$WORKSPACE_SEED_DIR/fingerprint" \
  "$WORKSPACE_SEED_DIR/vulnscan" \
  "$WORKSPACE_SEED_DIR/content-analysis" \
  "$WORKSPACE_SEED_DIR/ai-analysis"

cat >"$WORKSPACE_SEED_DIR/subdomain/subdomain-$TARGET.txt" <<'EOF'
app.example.com
admin.example.com
preview.example.com
api.example.com
EOF

cat >"$WORKSPACE_SEED_DIR/probing/http-$TARGET.txt" <<'EOF'
https://app.example.com/login
https://admin.example.com/preview
https://preview.example.com/graphql
https://api.example.com/v1/admin
EOF

cat >"$WORKSPACE_SEED_DIR/fingerprint/http-fingerprint-$TARGET.jsonl" <<'EOF'
{"url":"https://app.example.com/login","title":"Main Login","tech":["graphql","jwt","nginx"],"server":"nginx"}
{"url":"https://admin.example.com/preview","title":"Admin Preview","tech":["nextjs","jwt","node"],"framework":"nextjs"}
{"url":"https://preview.example.com/graphql","title":"Preview GraphQL","tech":["graphql","apollo","jwt"],"framework":"apollo"}
EOF

cat >"$WORKSPACE_SEED_DIR/content-analysis/js-endpoints-$TARGET.txt" <<'EOF'
https://app.example.com/api/session/refresh
https://preview.example.com/api/preview/tokens
https://admin.example.com/api/admin/export
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-jsonl-$TARGET.txt" <<'EOF'
{"template-id":"graphql-playground","info":{"severity":"high","name":"Exposed GraphQL playground"},"matched-at":"https://preview.example.com/graphql","type":"http"}
{"template-id":"jwt-none-alg","info":{"severity":"critical","name":"JWT none algorithm accepted"},"matched-at":"https://admin.example.com/preview","type":"http"}
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-critical-$TARGET.txt" <<'EOF'
[critical] jwt none algorithm accepted - https://admin.example.com/preview
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-high-$TARGET.txt" <<'EOF'
[high] graphql playground exposed - https://preview.example.com/graphql
[high] admin export route accessible - https://api.example.com/v1/admin
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate superdomain-extensive-ai-lite \
  >"$BASE_DIR/validate.log" 2>&1

run_args=(
  --base-folder "$BASE_DIR"
  --workflow-folder "$WORKFLOW_DIR"
  run
  -f superdomain-extensive-ai-lite
  -t "$TARGET"
  -p "enableSemanticSearch=true"
  -p "enableSemanticAgent=false"
  -p "enableVulnValidation=false"
  -p "enableRetestPlanning=false"
  -p "enableOperatorQueue=false"
  -p "enableCampaignHandoff=false"
  -p "enableCampaignCreate=false"
  -p "enableRetestQueue=false"
  -p "enableKnowledgeLearning=true"
  -p "enablePostVulnSemanticSearch=true"
  -p "includeKnowledgeBase=true"
  -p "knowledgeWorkspace=$KNOWLEDGE_WORKSPACE"
  -p "sharedKnowledgeWorkspace=$SHARED_WORKSPACE"
  -p "globalKnowledgeWorkspace=$GLOBAL_WORKSPACE"
)

excluded_modules=(
  osint
  subdomain
  dns-resolve
  http-probe
  fingerprint
  web-crawl
  content-analysis
  service-scan
  scan-backup
  scan-content
  vuln-suite
  ai-vuln-validation
  ai-code-review
  ai-retest-planning
  ai-operator-queue
  ai-campaign-handoff
  ai-targeted-rescan
  ai-retest-queue
  report
)

for module in "${excluded_modules[@]}"; do
  run_args+=(-x "$module")
done

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  "${run_args[@]}" \
  >"$BASE_DIR/run.log" 2>&1

WORKSPACE_DIR="$WORKSPACES_DIR/$TARGET"
if [[ ! -d "$WORKSPACE_DIR" ]]; then
  echo "expected workspace directory not found: $WORKSPACE_DIR" >&2
  exit 1
fi
WORKSPACE=$(basename "$WORKSPACE_DIR")
AI_DIR="$WORKSPACE_DIR/ai-analysis"

SEMANTIC_EARLY="$AI_DIR/semantic-search-early-$WORKSPACE.json"
SEMANTIC_POST="$AI_DIR/semantic-search-post-vuln-$WORKSPACE.json"
SEMANTIC_DECISION="$AI_DIR/semantic-search-decision-followup-$WORKSPACE.json"
SEMANTIC_KB_EARLY="$AI_DIR/knowledge-search-results-early-$WORKSPACE.json"
SEMANTIC_VECTOR_EARLY="$AI_DIR/vector-kb-search-results-early-$WORKSPACE.json"
AI_DECISION="$AI_DIR/ai-decision-$WORKSPACE.json"
APPLIED_DECISION="$AI_DIR/applied-ai-decision-$WORKSPACE.json"
FOLLOWUP_DECISION="$AI_DIR/followup-decision-$WORKSPACE.json"
KNOWLEDGE_LOG="$AI_DIR/knowledge-learning-$WORKSPACE.log"

for required_file in \
  "$SEMANTIC_EARLY" \
  "$SEMANTIC_POST" \
  "$SEMANTIC_DECISION" \
  "$SEMANTIC_KB_EARLY" \
  "$SEMANTIC_VECTOR_EARLY" \
  "$AI_DECISION" \
  "$APPLIED_DECISION" \
  "$FOLLOWUP_DECISION" \
  "$KNOWLEDGE_LOG"; do
  [[ -f "$required_file" ]] || {
    echo "missing expected artifact: $required_file" >&2
    exit 1
  }
done

semantic_early_total=$(jq -er '.total_results // 0' "$SEMANTIC_EARLY")
semantic_post_total=$(jq -er '.total_results // 0' "$SEMANTIC_POST")
semantic_decision_total=$(jq -er '.total_results // 0' "$SEMANTIC_DECISION")
semantic_early_kb_hits=$(jq -er 'length' "$SEMANTIC_KB_EARLY")
semantic_early_vector_hits=$(jq -er 'length' "$SEMANTIC_VECTOR_EARLY")

decision_focus_count=$(jq -er '.focus_areas | length' "$AI_DECISION")
decision_rescan_count=$(jq -er '.rescan_targets | length' "$AI_DECISION")
decision_reasoning=$(jq -er '.reasoning' "$AI_DECISION")

applied_source_kind=$(jq -er '.source.kind' "$APPLIED_DECISION")
applied_followup_used=$(jq -r '(.source.followup_used // false) | tostring' "$APPLIED_DECISION")
applied_profile=$(jq -er '.scan.profile' "$APPLIED_DECISION")
applied_rescan_count=$(jq -er '.targets.rescan_targets | length' "$APPLIED_DECISION")

followup_next_phase=$(jq -er '.execution_feedback.next_phase' "$FOLLOWUP_DECISION")
followup_priority_mode=$(jq -er '.seed_focus.priority_mode' "$FOLLOWUP_DECISION")
followup_campaign_targets=$(jq -er '.followup_summary.campaign_targets' "$FOLLOWUP_DECISION")
followup_operator_tasks=$(jq -er '.followup_summary.operator_tasks' "$FOLLOWUP_DECISION")
followup_retest_targets=$(jq -er '.followup_summary.retest_targets' "$FOLLOWUP_DECISION")

assert_ge "$semantic_early_total" 1 "semantic early total results"
assert_ge "$semantic_post_total" 1 "semantic post-vuln total results"
assert_ge "$semantic_decision_total" 1 "semantic decision-followup total results"
assert_ge "$semantic_early_kb_hits" 1 "semantic early kb hits"
assert_ge "$semantic_early_vector_hits" 1 "semantic early vector kb hits"
assert_ge "$decision_focus_count" 1 "ai decision focus areas"
assert_ge "$decision_rescan_count" 1 "ai decision rescan targets"
assert_contains "$decision_reasoning" "knowledge context hits" "ai decision reasoning"
assert_eq "$applied_source_kind" "ai-decision" "applied decision source kind"
assert_eq "$applied_followup_used" "false" "applied decision follow-up usage"
assert_ge "$applied_rescan_count" 1 "applied decision rescan targets"
assert_contains "$applied_profile" "focused" "applied decision scan profile"
assert_eq "$followup_next_phase" "knowledge-consolidation" "follow-up next phase"
assert_eq "$followup_priority_mode" "knowledge-first" "follow-up priority mode"
assert_eq "$followup_campaign_targets" "0" "follow-up campaign targets"
assert_eq "$followup_operator_tasks" "0" "follow-up operator tasks"
assert_eq "$followup_retest_targets" "0" "follow-up retest targets"

kb_docs_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb docs -w "$WORKSPACE")
kb_docs_count=$(printf '%s' "$kb_docs_json" | jq -er '.data | length')
kb_search_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb search -w "$WORKSPACE" --query "knowledge-consolidation")
kb_search_count=$(printf '%s' "$kb_search_json" | jq -er 'length')

assert_ge "$kb_docs_count" 2 "knowledge workspace document count after autolearn"
assert_ge "$kb_search_count" 1 "knowledge search learned hit count"

knowledge_log=$(cat "$KNOWLEDGE_LOG")
assert_contains "$knowledge_log" "Knowledge learning completed" "knowledge learning log"
assert_contains "$knowledge_log" "Base folder: $BASE_DIR" "knowledge learning base folder"
assert_contains "$knowledge_log" "Scope: workspace" "knowledge learning scope"

echo "ok superdomain lite flow smoke regression: current-source lite flow executes semantic decision and knowledge autolearn closure"
