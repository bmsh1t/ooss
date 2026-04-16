#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-ai-workflow-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/ai-smoke}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
TARGET="${TARGET:-smoke-ai-regression.example.com}"

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

dump_logs() {
  if [[ -f "$BASE_DIR/validate.log" ]]; then
    echo "---- validate.log ----" >&2
    tail -n 80 "$BASE_DIR/validate.log" >&2 || true
  fi
  if [[ -f "$BASE_DIR/run.log" ]]; then
    echo "---- run.log ----" >&2
    tail -n 160 "$BASE_DIR/run.log" >&2 || true
  fi
}

trap 'rc=$?; if [[ $rc -ne 0 ]]; then dump_logs; fi' EXIT

require_cmd jq

if [[ ! -d "$WORKFLOW_DIR" ]]; then
  echo "workflow directory not found: $WORKFLOW_DIR" >&2
  exit 1
fi

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

cd "$ROOT_DIR"
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
  enabled: false
  auto_index_on_ingest: false
  auto_index_on_learn: false
server:
  enabled_auth_api: false
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate superdomain-ai-followup-smoke-flow \
  >"$BASE_DIR/validate.log" 2>&1

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  run -f superdomain-ai-followup-smoke-flow -t "$TARGET" \
  >"$BASE_DIR/run.log" 2>&1

WORKSPACE_DIR=$(find "$WORKSPACES_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)
if [[ -z "${WORKSPACE_DIR:-}" ]]; then
  echo "no workspace directory created under $WORKSPACES_DIR" >&2
  exit 1
fi
WORKSPACE=$(basename "$WORKSPACE_DIR")
AI_DIR="$WORKSPACE_DIR/ai-analysis"

AI_DECISION="$AI_DIR/ai-decision-$WORKSPACE.json"
APPLIED_DECISION="$AI_DIR/applied-ai-decision-$WORKSPACE.json"
RETEST_PLAN="$AI_DIR/retest-plan-$WORKSPACE.json"
OPERATOR_QUEUE="$AI_DIR/operator-queue-$WORKSPACE.json"
CAMPAIGN_HANDOFF="$AI_DIR/campaign-handoff-$WORKSPACE.json"
CAMPAIGN_CREATE="$AI_DIR/campaign-create-$WORKSPACE.json"
RETEST_QUEUE="$AI_DIR/retest-queue-summary-$WORKSPACE.json"
FOLLOWUP_DECISION="$AI_DIR/followup-decision-$WORKSPACE.json"
KNOWLEDGE_CONTEXT="$AI_DIR/unified-analysis-knowledge-$WORKSPACE.json"
KNOWLEDGE_LOG="$AI_DIR/knowledge-learning-$WORKSPACE.log"

for required_file in \
  "$AI_DECISION" \
  "$APPLIED_DECISION" \
  "$RETEST_PLAN" \
  "$OPERATOR_QUEUE" \
  "$CAMPAIGN_HANDOFF" \
  "$CAMPAIGN_CREATE" \
  "$RETEST_QUEUE" \
  "$FOLLOWUP_DECISION" \
  "$KNOWLEDGE_CONTEXT" \
  "$KNOWLEDGE_LOG"; do
  [[ -f "$required_file" ]] || {
    echo "missing expected artifact: $required_file" >&2
    exit 1
  }
done

decision_prev_source=$(jq -er '.decision_inputs.previous_followup_source_kind' "$AI_DECISION")
decision_rescan_count=$(jq -er '.rescan_targets | length' "$AI_DECISION")
decision_passive_signal_count=$(jq -er '.decision_inputs.passive_web_risk_signals // 0' "$AI_DECISION")
decision_passive_target_count=$(jq -er '.decision_inputs.passive_target_count // 0' "$AI_DECISION")
applied_followup_used=$(jq -er '.source.followup_used' "$APPLIED_DECISION")
applied_followup_kind=$(jq -er '.source.followup_source_kind' "$APPLIED_DECISION")
retest_total=$(jq -er '.summary.total_targets // 0' "$RETEST_PLAN")
retest_decision_source=$(jq -er '.summary.decision_source_kind // ""' "$RETEST_PLAN")
retest_decision_targets=$(jq -er '.summary.decision_target_count // 0' "$RETEST_PLAN")
operator_total=$(jq -er '.summary.total_tasks // 0' "$OPERATOR_QUEUE")
operator_decision_source=$(jq -er '.summary.decision_source_kind // ""' "$OPERATOR_QUEUE")
operator_decision_targets=$(jq -er '.summary.decision_target_count // 0' "$OPERATOR_QUEUE")
campaign_ready=$(jq -er '.handoff_ready' "$CAMPAIGN_HANDOFF")
campaign_decision_focus_targets=$(jq -er '.counts.decision_focus_targets // 0' "$CAMPAIGN_HANDOFF")
campaign_target_count=$(jq -er '.counts.campaign_targets' "$CAMPAIGN_HANDOFF")
campaign_create_status=$(jq -er '.status' "$CAMPAIGN_CREATE")
campaign_create_id=$(jq -er '.campaign_id' "$CAMPAIGN_CREATE")
campaign_create_runs=$(jq -er '.queued_runs' "$CAMPAIGN_CREATE")
retest_queue_status=$(jq -er '.status' "$RETEST_QUEUE")
retest_queue_targets=$(jq -er '.queued_targets' "$RETEST_QUEUE")
followup_next_phase=$(jq -er '.execution_feedback.next_phase' "$FOLLOWUP_DECISION")
followup_campaign_status=$(jq -er '.followup_summary.campaign_create_status' "$FOLLOWUP_DECISION")
followup_passive_targets=$(jq -er '.followup_summary.passive_targets // 0' "$FOLLOWUP_DECISION")
followup_priority_mode=$(jq -er '.seed_focus.priority_mode' "$FOLLOWUP_DECISION")
followup_reuse_sources=$(jq -er '(.seed_focus.reuse_sources // []) | join(",")' "$FOLLOWUP_DECISION")
knowledge_context_workspace=$(jq -er '.workspace' "$KNOWLEDGE_CONTEXT")
knowledge_context_learning_workspace=$(jq -er '.learning_workspace' "$KNOWLEDGE_CONTEXT")
knowledge_context_retrieval_workspace=$(jq -er '.retrieval_workspace' "$KNOWLEDGE_CONTEXT")

assert_eq "$decision_prev_source" "decision-file" "ai decision previous follow-up source"
assert_ge "$decision_rescan_count" 2 "ai decision rescan target count"
assert_ge "$decision_passive_signal_count" 4 "ai decision passive signal count"
assert_ge "$decision_passive_target_count" 3 "ai decision passive target count"
assert_eq "$applied_followup_used" "true" "applied decision follow-up usage"
assert_eq "$applied_followup_kind" "decision-file" "applied decision follow-up source kind"
assert_ge "$retest_total" 2 "retest plan total targets"
assert_eq "$retest_decision_source" "applied-ai-decision" "retest plan decision source"
assert_ge "$retest_decision_targets" 2 "retest plan decision target count"
assert_ge "$operator_total" 2 "operator queue total tasks"
assert_eq "$operator_decision_source" "applied-ai-decision" "operator queue decision source"
assert_ge "$operator_decision_targets" 2 "operator queue decision target count"
assert_eq "$campaign_ready" "true" "campaign handoff readiness"
assert_ge "$campaign_decision_focus_targets" 2 "campaign handoff decision focus target count"
assert_ge "$campaign_target_count" 3 "campaign handoff target count"
assert_eq "$campaign_create_status" "created" "campaign creation status"
assert_ge "$campaign_create_runs" 3 "campaign queued run count"
assert_eq "$retest_queue_status" "queued" "retest queue status"
assert_ge "$retest_queue_targets" 2 "retest queued target count"
assert_eq "$followup_campaign_status" "created" "follow-up campaign status"
assert_ge "$followup_passive_targets" 3 "follow-up passive target count"
assert_eq "$followup_priority_mode" "manual-first" "follow-up priority mode"
assert_eq "$followup_next_phase" "manual-exploitation" "follow-up next phase"
assert_contains "$followup_reuse_sources" "passive-web-risk" "follow-up reuse sources"
assert_eq "$knowledge_context_workspace" "$WORKSPACE" "knowledge context workspace"
assert_eq "$knowledge_context_learning_workspace" "$WORKSPACE" "knowledge learning workspace"
assert_eq "$knowledge_context_retrieval_workspace" "shared-kb" "knowledge retrieval workspace"

campaign_status_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" campaign status "$campaign_create_id")
campaign_status_total=$(printf '%s' "$campaign_status_json" | jq -er '.summary.targets_total')
campaign_status_auto_deep_scan=$(printf '%s' "$campaign_status_json" | jq -er '.campaign.auto_deep_scan')
assert_ge "$campaign_status_total" 3 "campaign status total targets"
assert_eq "$campaign_status_auto_deep_scan" "true" "campaign auto deep scan flag"

queued_runs_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" query runs --workflow ai-followup-target-module)
queued_runs_count=$(printf '%s' "$queued_runs_json" | jq -er 'length')
assert_ge "$queued_runs_count" 3 "queued follow-up run count"

kb_docs_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb docs -w "$WORKSPACE")
kb_docs_count=$(printf '%s' "$kb_docs_json" | jq -er '.data | length')
assert_ge "$kb_docs_count" 3 "knowledge learned document count"

kb_search_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" kb search -w "$WORKSPACE" --query "operator queue")
kb_search_count=$(printf '%s' "$kb_search_json" | jq -er 'length')
assert_ge "$kb_search_count" 1 "knowledge search hit count"

knowledge_log=$(cat "$KNOWLEDGE_LOG")
assert_contains "$knowledge_log" "Knowledge learning completed" "knowledge learning log"
assert_contains "$knowledge_log" "Base folder: $BASE_DIR" "knowledge learning base folder"
assert_contains "$knowledge_log" "Learning workspace: $WORKSPACE" "knowledge learning source workspace log"
assert_contains "$knowledge_log" "Retrieval workspace: shared-kb" "knowledge learning retrieval workspace log"

echo "ok ai workflow smoke regression: intelligent-analysis, follow-up packaging, queueing, and knowledge autolearn"
