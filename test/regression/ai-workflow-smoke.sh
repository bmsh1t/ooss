#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-ai-workflow-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
TARGET="${TARGET:-smoke-ai-regression.example.com}"
SMOKE_FLOW_PATH="${SMOKE_FLOW_PATH:-${WORKFLOW_DIR}/ai-smoke/superdomain-ai-followup-smoke-flow.yaml}"

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

if [[ ! -f "$SMOKE_FLOW_PATH" ]]; then
  echo "smoke workflow not found: $SMOKE_FLOW_PATH" >&2
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
  workflow validate "$SMOKE_FLOW_PATH" \
  >"$BASE_DIR/validate.log" 2>&1

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  run -f "$SMOKE_FLOW_PATH" -t "$TARGET" \
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
RESUME_CONTEXT="$AI_DIR/resume-context-$WORKSPACE.json"
NEXT_ACTIONS="$AI_DIR/next-actions-$WORKSPACE.json"
OPERATOR_SUMMARY="$AI_DIR/operator-summary-$WORKSPACE.md"
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
  "$RESUME_CONTEXT" \
  "$NEXT_ACTIONS" \
  "$OPERATOR_SUMMARY" \
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
retest_prev_source=$(jq -er '.summary.previous_followup_source_kind // ""' "$RETEST_PLAN")
retest_prev_queue_effective=$(jq -er '.summary.previous_followup_queue_followup_effective' "$RETEST_PLAN")
retest_automation_queue=$(jq -er '.automation_queue | length' "$RETEST_PLAN")
operator_total=$(jq -er '.summary.total_tasks // 0' "$OPERATOR_QUEUE")
operator_decision_source=$(jq -er '.summary.decision_source_kind // ""' "$OPERATOR_QUEUE")
operator_decision_targets=$(jq -er '.summary.decision_target_count // 0' "$OPERATOR_QUEUE")
operator_prev_source=$(jq -er '.summary.previous_followup_source_kind // ""' "$OPERATOR_QUEUE")
operator_prev_mode=$(jq -er '.summary.previous_priority_mode // ""' "$OPERATOR_QUEUE")
operator_focus_first=$(jq -er '.focus_targets[0] // ""' "$OPERATOR_QUEUE")
operator_task_first_title=$(jq -er '.tasks[0].title // ""' "$OPERATOR_QUEUE")
campaign_ready=$(jq -er '.handoff_ready' "$CAMPAIGN_HANDOFF")
campaign_decision_focus_targets=$(jq -er '.counts.decision_focus_targets // 0' "$CAMPAIGN_HANDOFF")
campaign_target_count=$(jq -er '.counts.campaign_targets' "$CAMPAIGN_HANDOFF")
campaign_prev_source=$(jq -er '.campaign_profile.previous_followup_source_kind // ""' "$CAMPAIGN_HANDOFF")
campaign_create_status=$(jq -er '.status' "$CAMPAIGN_CREATE")
campaign_create_id=$(jq -er '.campaign_id' "$CAMPAIGN_CREATE")
campaign_create_runs=$(jq -er '.queued_runs' "$CAMPAIGN_CREATE")
retest_queue_status=$(jq -er '.status' "$RETEST_QUEUE")
retest_queue_reason=$(jq -er '.reason // ""' "$RETEST_QUEUE")
retest_queue_targets=$(jq -er '.queued_targets' "$RETEST_QUEUE")
retest_queue_prev_source=$(jq -er '.previous_followup_source_kind // ""' "$RETEST_QUEUE")
followup_next_phase=$(jq -er '.execution_feedback.next_phase' "$FOLLOWUP_DECISION")
followup_campaign_status=$(jq -er '.followup_summary.campaign_create_status' "$FOLLOWUP_DECISION")
followup_resume_suppressed=$(jq -er '(.followup_summary.resume_suppressed_actions // []) | join(",")' "$FOLLOWUP_DECISION")
followup_reused_actions=$(jq -er '(.followup_summary.reused_actions // []) | join(",")' "$FOLLOWUP_DECISION")
followup_skipped_duplicates=$(jq -er '(.followup_summary.skipped_duplicate_actions // []) | join(",")' "$FOLLOWUP_DECISION")
followup_passive_targets=$(jq -er '.followup_summary.passive_targets // 0' "$FOLLOWUP_DECISION")
followup_priority_mode=$(jq -er '.seed_focus.priority_mode' "$FOLLOWUP_DECISION")
followup_reuse_sources=$(jq -er '(.seed_focus.reuse_sources // []) | join(",")' "$FOLLOWUP_DECISION")
resume_source=$(jq -er '.followup_decision_source' "$RESUME_CONTEXT")
resume_next_phase=$(jq -er '.next_phase' "$RESUME_CONTEXT")
resume_priority_mode=$(jq -er '.priority_mode' "$RESUME_CONTEXT")
resume_campaign_status=$(jq -er '.campaign_create.status' "$RESUME_CONTEXT")
resume_passive_targets=$(jq -er '.followup_summary.passive_targets // 0' "$RESUME_CONTEXT")
resume_reuse_sources=$(jq -er '(.reuse_sources // []) | join(",")' "$RESUME_CONTEXT")
next_actions_count=$(jq -er 'if type == "array" then length else error("next-actions must be an array") end' "$NEXT_ACTIONS")
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
assert_eq "$retest_prev_source" "resume-context" "retest plan previous follow-up source"
assert_eq "$retest_prev_queue_effective" "true" "retest plan previous queue effectiveness"
assert_eq "$retest_automation_queue" "0" "retest plan automation queue suppressed by resume gate"
assert_ge "$operator_total" 2 "operator queue total tasks"
assert_eq "$operator_decision_source" "applied-ai-decision" "operator queue decision source"
assert_ge "$operator_decision_targets" 2 "operator queue decision target count"
assert_eq "$operator_prev_source" "resume-context" "operator queue previous follow-up source"
assert_eq "$operator_prev_mode" "manual-first" "operator queue previous priority mode"
assert_contains "$operator_focus_first" "/admin" "operator queue manual-first focus ordering"
assert_eq "$operator_task_first_title" "Resume manual exploit path" "operator queue manual exploit task title"
assert_eq "$campaign_ready" "true" "campaign handoff readiness"
assert_ge "$campaign_decision_focus_targets" 2 "campaign handoff decision focus target count"
assert_ge "$campaign_target_count" 3 "campaign handoff target count"
assert_eq "$campaign_prev_source" "resume-context" "campaign handoff previous follow-up source"
assert_eq "$campaign_create_status" "created" "campaign creation status"
assert_ge "$campaign_create_runs" 3 "campaign queued run count"
assert_eq "$retest_queue_status" "skipped" "retest queue status"
assert_eq "$retest_queue_reason" "resume_queue_already_effective" "retest queue resume gate reason"
assert_eq "$retest_queue_prev_source" "resume-context" "retest queue previous follow-up source"
assert_eq "$retest_queue_targets" "0" "retest queued target count after resume gate"
assert_eq "$followup_campaign_status" "created" "follow-up campaign status"
assert_eq "$followup_resume_suppressed" "retest-queue" "follow-up resume suppressed actions"
assert_contains "$followup_skipped_duplicates" "retest-queue" "follow-up skipped duplicate retest queue"
assert_ge "$followup_passive_targets" 3 "follow-up passive target count"
assert_eq "$followup_priority_mode" "manual-first" "follow-up priority mode"
assert_eq "$followup_next_phase" "manual-exploitation" "follow-up next phase"
assert_contains "$followup_reuse_sources" "passive-web-risk" "follow-up reuse sources"
assert_eq "$resume_source" "followup-decision" "resume context source"
assert_eq "$resume_priority_mode" "manual-first" "resume context priority mode"
assert_eq "$resume_next_phase" "manual-exploitation" "resume context next phase"
assert_eq "$resume_campaign_status" "created" "resume context campaign status"
assert_ge "$resume_passive_targets" 3 "resume context passive target count"
assert_contains "$resume_reuse_sources" "passive-web-risk" "resume context reuse sources"
assert_ge "$next_actions_count" 1 "next actions count"
grep -q 'Next phase: manual-exploitation' "$OPERATOR_SUMMARY"
grep -q 'Campaign status: created' "$OPERATOR_SUMMARY"
assert_eq "$knowledge_context_workspace" "$WORKSPACE" "knowledge context workspace"
assert_eq "$knowledge_context_learning_workspace" "$WORKSPACE" "knowledge learning workspace"
assert_eq "$knowledge_context_retrieval_workspace" "shared-kb" "knowledge retrieval workspace"

campaign_status_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" campaign status "$campaign_create_id")
campaign_status_total=$(printf '%s' "$campaign_status_json" | jq -er '.summary.targets_total')
campaign_status_auto_deep_scan=$(printf '%s' "$campaign_status_json" | jq -er '.campaign.auto_deep_scan')
assert_ge "$campaign_status_total" 3 "campaign status total targets"
assert_eq "$campaign_status_auto_deep_scan" "true" "campaign auto deep scan flag"

queued_runs_json=$(OSM_SKIP_PATH_SETUP=1 OSM_WORKSPACES="$WORKSPACES_DIR" "$OSMEDEUS_BIN" --silent --json --base-folder "$BASE_DIR" --workflow-folder "$WORKFLOW_DIR" query runs --workflow "ai-smoke/ai-followup-target-module.yaml")
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
