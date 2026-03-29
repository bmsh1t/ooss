#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-superdomain-stable-flow-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
EMBED_PORT="${EMBED_PORT:-8914}"
TARGET="${TARGET:-example.com}"
FLOW_NAME="${FLOW_NAME:-superdomain-extensive-ai-stable}"
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
    echo "assertion failed for ${label}: expected content to contain '${needle}'" >&2
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

assert_non_empty() {
  local value="$1"
  local label="$2"
  if [[ -z "$value" ]]; then
    echo "assertion failed for ${label}: value is empty" >&2
    exit 1
  fi
}

count_non_empty_lines() {
  local file="$1"
  if [[ -f "$file" ]]; then
    awk 'NF { count++ } END { print count+0 }' "$file" 2>/dev/null || echo 0
  else
    echo 0
  fi
}

count_retest_targets() {
  local file="$1"
  if [[ -f "$file" ]] && jq empty "$file" >/dev/null 2>&1; then
    jq -r '
      if has("targets") or has("automation_queue") then
        [((.targets[]? | if type == "object" then .target else . end)), ((.automation_queue[]? | if type == "object" then .target else . end))]
        | reduce .[] as $item ([]; if ($item != null and $item != "" and (index($item) == null)) then . + [$item] else . end)
        | length
      else
        (.summary.total_targets // 0)
      end
    ' "$file" 2>/dev/null || echo 0
  else
    echo 0
  fi
}

count_operator_tasks() {
  local file="$1"
  if [[ -f "$file" ]] && jq empty "$file" >/dev/null 2>&1; then
    jq -r 'if has("tasks") then ([.tasks[]?] | length) else (.summary.total_tasks // 0) end' "$file" 2>/dev/null || echo 0
  else
    echo 0
  fi
}

count_campaign_targets() {
  local file="$1"
  if [[ -f "$file" ]] && jq empty "$file" >/dev/null 2>&1; then
    jq -r '
      if has("targets") then
        [
          (.targets.decision_rescan[]?),
          (.targets.retest[]?),
          (.targets.operator_focus[]?),
          (.targets.semantic_priority[]?),
          (.targets.previous_followup[]?)
        ]
        | reduce .[] as $item ([]; if ($item != null and $item != "" and (index($item) == null)) then . + [$item] else . end)
        | length
      else
        (.counts.campaign_targets // 0)
      end
    ' "$file" 2>/dev/null || echo 0
  else
    echo 0
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
      tail -n 200 "$file" >&2 || true
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
  "$WORKSPACE_SEED_DIR/vuln-scan-suite" \
  "$WORKSPACE_SEED_DIR/content-analysis" \
  "$WORKSPACE_SEED_DIR/osint" \
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
{"url":"https://api.example.com/v1/admin","title":"Admin API","tech":["go","rest","jwt"],"framework":"chi"}
EOF

cat >"$WORKSPACE_SEED_DIR/content-analysis/js-endpoints-$TARGET.txt" <<'EOF'
https://app.example.com/api/session/refresh
https://preview.example.com/api/preview/tokens
https://admin.example.com/api/admin/export
EOF

cat >"$WORKSPACE_SEED_DIR/osint/emails-$TARGET.txt" <<'EOF'
security@example.com
admin@example.com
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-jsonl-$TARGET.txt" <<'EOF'
{"template-id":"graphql-playground","info":{"severity":"high","name":"Exposed GraphQL playground"},"matched-at":"https://preview.example.com/graphql","type":"http"}
{"template-id":"jwt-none-alg","info":{"severity":"critical","name":"JWT none algorithm accepted"},"matched-at":"https://admin.example.com/preview","type":"http"}
{"template-id":"admin-panel-exposure","info":{"severity":"high","name":"Admin export route accessible"},"matched-at":"https://api.example.com/v1/admin","type":"http"}
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-critical-$TARGET.txt" <<'EOF'
[critical] jwt none algorithm accepted - https://admin.example.com/preview
EOF

cat >"$WORKSPACE_SEED_DIR/vulnscan/nuclei-high-$TARGET.txt" <<'EOF'
[high] graphql playground exposed - https://preview.example.com/graphql
[high] admin export route accessible - https://api.example.com/v1/admin
EOF

cat >"$WORKSPACE_SEED_DIR/vuln-scan-suite/sqli-$TARGET.txt" <<'EOF'
[high] possible sql injection on preview token endpoint - https://preview.example.com/api/preview/tokens?id=1'
EOF

cat >"$WORKSPACE_SEED_DIR/vuln-scan-suite/ssrf-$TARGET.txt" <<'EOF'
[high] backend fetcher can request internal hosts - https://app.example.com/api/session/refresh
EOF

cat >"$WORKSPACE_SEED_DIR/vuln-scan-suite/ssti-$TARGET.txt" <<'EOF'
[medium] template injection indicator in preview renderer - https://admin.example.com/preview
EOF

cat >"$WORKSPACE_SEED_DIR/vuln-scan-suite/secrets-$TARGET.txt" <<'EOF'
[medium] leaked preview service token in JavaScript bundle - https://preview.example.com/graphql
EOF

cat >"$WORKSPACE_SEED_DIR/vuln-scan-suite/takeover-$TARGET.txt" <<'EOF'
[high] dangling admin preview cname candidate - admin.example.com
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate "$FLOW_NAME" \
  >"$BASE_DIR/validate.log" 2>&1

run_args=(
  --base-folder "$BASE_DIR"
  --workflow-folder "$WORKFLOW_DIR"
  run
  -f "$FLOW_NAME"
  -t "$TARGET"
  -p "enablePreScanDecision=false"
  -p "enableSemanticSearch=true"
  -p "enableSemanticAgent=false"
  -p "enableWafBypass=false"
  -p "enableVulnValidation=true"
  -p "enableAttackChain=true"
  -p "enablePathPlanning=true"
  -p "enableKnowledgeLearning=true"
  -p "enablePostVulnSemanticSearch=true"
  -p "enableCampaignCreate=false"
  -p "enableRetestQueue=false"
  -p "includeKnowledgeBase=true"
  -p "knowledgeWorkspace=$KNOWLEDGE_WORKSPACE"
  -p "sharedKnowledgeWorkspace=$SHARED_WORKSPACE"
  -p "globalKnowledgeWorkspace=$GLOBAL_WORKSPACE"
  -p "agentTimeout=10s"
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
  ai-pre-scan-decision
  ai-waf-bypass
  ai-code-review
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
VULN_VALIDATION="$AI_DIR/vuln-validation-$WORKSPACE.json"
ATTACK_CHAIN_JSON="$AI_DIR/attack-chain-$WORKSPACE.json"
ATTACK_CHAIN_DIAGRAM="$AI_DIR/attack-chain-diagram-$WORKSPACE.md"
ATTACK_CHAIN_VIEW="$AI_DIR/attack-chain-view-$WORKSPACE.txt"
PATH_PLANNING_JSON="$AI_DIR/path-planning-$WORKSPACE.json"
ATTACK_PLAN_MD="$AI_DIR/attack-plan-$WORKSPACE.md"
EXECUTION_CHECKLIST="$AI_DIR/execution-checklist-$WORKSPACE.txt"
SKILLS_COMBINED="$AI_DIR/skills-combined.md"
SECURITY_GUIDE="$AI_DIR/security-testing-guide.md"
AI_DECISION="$AI_DIR/ai-decision-$WORKSPACE.json"
APPLIED_DECISION="$AI_DIR/applied-ai-decision-$WORKSPACE.json"
RETEST_PLAN="$AI_DIR/retest-plan-$WORKSPACE.json"
OPERATOR_QUEUE="$AI_DIR/operator-queue-$WORKSPACE.json"
CAMPAIGN_HANDOFF="$AI_DIR/campaign-handoff-$WORKSPACE.json"
FOLLOWUP_DECISION="$AI_DIR/followup-decision-$WORKSPACE.json"
KNOWLEDGE_LOG="$AI_DIR/knowledge-learning-$WORKSPACE.log"

for required_file in \
  "$SEMANTIC_EARLY" \
  "$SEMANTIC_POST" \
  "$SEMANTIC_DECISION" \
  "$SEMANTIC_KB_EARLY" \
  "$SEMANTIC_VECTOR_EARLY" \
  "$VULN_VALIDATION" \
  "$ATTACK_CHAIN_JSON" \
  "$ATTACK_CHAIN_DIAGRAM" \
  "$ATTACK_CHAIN_VIEW" \
  "$PATH_PLANNING_JSON" \
  "$ATTACK_PLAN_MD" \
  "$EXECUTION_CHECKLIST" \
  "$SKILLS_COMBINED" \
  "$SECURITY_GUIDE" \
  "$AI_DECISION" \
  "$APPLIED_DECISION" \
  "$RETEST_PLAN" \
  "$OPERATOR_QUEUE" \
  "$CAMPAIGN_HANDOFF" \
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

vuln_total=$(jq -er '.total_validated // (.findings | length) // 0' "$VULN_VALIDATION")
vuln_actionable_total=$(jq -er '(.confirmed_real // 0) + (.needs_manual_verification // 0)' "$VULN_VALIDATION")

attack_chain_total=$(jq -er '.attack_chain_summary.total_chains // ([.attack_chains[]?] | length) // 0' "$ATTACK_CHAIN_JSON")
attack_chain_entry_points=$(jq -er '[.attack_chain_summary.most_likely_entry_points[]?] | length' "$ATTACK_CHAIN_JSON")
path_phase_count=$(jq -er '.plan_summary.total_phases // ([.execution_phases[]?] | length) // 0' "$PATH_PLANNING_JSON")
path_checklist_count=$(jq -er '[.verification_checklist[]?] | length' "$PATH_PLANNING_JSON")

decision_focus_count=$(jq -er '.focus_areas | length' "$AI_DECISION")
decision_rescan_count=$(jq -er '.rescan_targets | length' "$AI_DECISION")

applied_source_kind=$(jq -er '.source.kind' "$APPLIED_DECISION")
applied_followup_used=$(jq -r '(.source.followup_used // false) | tostring' "$APPLIED_DECISION")
applied_profile=$(jq -er '.scan.profile' "$APPLIED_DECISION")
applied_rescan_count=$(jq -er '.targets.rescan_targets | length' "$APPLIED_DECISION")

followup_next_phase=$(jq -r '.execution_feedback.next_phase // .seed_focus.next_phase // ""' "$FOLLOWUP_DECISION")
followup_priority_mode=$(jq -r '.seed_focus.priority_mode // ""' "$FOLLOWUP_DECISION")

retest_target_count=$(count_retest_targets "$RETEST_PLAN")
operator_task_count=$(count_operator_tasks "$OPERATOR_QUEUE")
campaign_target_count=$(count_campaign_targets "$CAMPAIGN_HANDOFF")
followup_action_total=$((retest_target_count + operator_task_count + campaign_target_count))

attack_chain_diagram=$(cat "$ATTACK_CHAIN_DIAGRAM")
attack_chain_view_lines=$(count_non_empty_lines "$ATTACK_CHAIN_VIEW")
attack_plan_md=$(cat "$ATTACK_PLAN_MD")
execution_checklist_lines=$(count_non_empty_lines "$EXECUTION_CHECKLIST")
skills_combined=$(cat "$SKILLS_COMBINED")
skills_combined_lines=$(count_non_empty_lines "$SKILLS_COMBINED")
security_guide=$(cat "$SECURITY_GUIDE")
security_guide_lines=$(count_non_empty_lines "$SECURITY_GUIDE")

kb_docs_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb docs -w "$WORKSPACE")
kb_docs_count=$(printf '%s' "$kb_docs_json" | jq -er '.data | length')
kb_generated_docs=$(printf '%s' "$kb_docs_json" | jq -er '[.data[] | select(.source_type == "generated")] | length')
kb_docs_text=$(printf '%s' "$kb_docs_json")

assert_ge "$semantic_early_total" 1 "semantic early total results"
assert_ge "$semantic_post_total" 1 "semantic post-vuln total results"
assert_ge "$semantic_decision_total" 1 "semantic decision-followup total results"
assert_ge "$semantic_early_kb_hits" 1 "semantic early kb hits"
assert_ge "$semantic_early_vector_hits" 1 "semantic early vector kb hits"
assert_ge "$vuln_total" 1 "vulnerability validation total findings"
assert_ge "$vuln_actionable_total" 1 "vulnerability validation actionable findings"
assert_ge "$attack_chain_total" 1 "attack chain count"
assert_ge "$attack_chain_entry_points" 1 "attack chain entry points"
assert_contains "$attack_chain_diagram" "graph TD" "attack chain diagram"
assert_ge "$attack_chain_view_lines" 5 "attack chain text view lines"
assert_ge "$path_phase_count" 1 "path planning phase count"
assert_ge "$path_checklist_count" 1 "path planning checklist count"
assert_contains "$attack_plan_md" "## 执行阶段" "attack plan markdown"
assert_ge "$execution_checklist_lines" 1 "execution checklist line count"
assert_ge "$skills_combined_lines" 10 "skills combined line count"
assert_contains "$skills_combined" "API Security Testing" "skills combined api skill"
assert_ge "$security_guide_lines" 20 "security guide line count"
assert_contains "$security_guide" "Web Application Testing" "security guide section"
assert_ge "$decision_focus_count" 1 "ai decision focus areas"
assert_ge "$decision_rescan_count" 1 "ai decision rescan targets"
assert_eq "$applied_source_kind" "ai-decision" "applied decision source kind"
assert_eq "$applied_followup_used" "false" "applied decision follow-up usage"
assert_ge "$applied_rescan_count" 1 "applied decision rescan targets"
assert_contains "$applied_profile" "focused" "applied decision scan profile"
assert_non_empty "$followup_next_phase" "follow-up next phase"
assert_non_empty "$followup_priority_mode" "follow-up priority mode"
assert_ge "$followup_action_total" 1 "combined follow-up action count"
assert_ge "$kb_docs_count" 4 "knowledge workspace document count after autolearn"
assert_ge "$kb_generated_docs" 3 "generated knowledge document count after autolearn"
assert_contains "$kb_docs_text" "attack-chain-example.com.json" "knowledge docs attack chain metadata"

knowledge_log=$(cat "$KNOWLEDGE_LOG")
assert_contains "$knowledge_log" "Knowledge learning completed" "knowledge learning log"
assert_contains "$knowledge_log" "Base folder: $BASE_DIR" "knowledge learning base folder"
assert_contains "$knowledge_log" "Scope: workspace" "knowledge learning scope"

echo "ok superdomain flow smoke regression: $FLOW_NAME executes acp-backed fallback chain, workbench import, and knowledge autolearn closure"
