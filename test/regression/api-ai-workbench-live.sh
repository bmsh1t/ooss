#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-api-ai-workbench-live}"
PORT="${PORT:-8906}"
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
require_cmd awk
require_cmd grep
require_cmd wc

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

jq -nc '{
  attack_chain_summary: {
    total_chains: 1,
    critical_chains: 0,
    high_impact_chains: 1,
    most_likely_entry_points: ["SQL Injection @ /login"]
  },
  attack_chains: [
    {
      chain_id: "chain-login",
      chain_name: "Login SQLi Chain",
      entry_point: {
        vulnerability: "SQL Injection",
        url: "https://api-regression.example.com/login",
        severity: "high"
      },
      chain_steps: [
        {
          step: 1,
          action: "exploit login form"
        }
      ],
      final_objective: "Dump user data",
      difficulty: "medium",
      impact: "high",
      success_probability: 0.8
    }
  ],
  critical_paths: [],
  defense_recommendations: ["Monitor login anomalies"]
}' >"$BASE_DIR/attack-chain.json"

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

WORKSPACE="api-regression.example.com"
TARGET="https://api-regression.example.com/login"
LOW_TARGET="https://api-regression-low.example.com"

bulk_triage_body=$(jq -nc --arg ws "$WORKSPACE" '{
  workspace: $ws,
  vuln_info: "open-redirect",
  vuln_title: "Open Redirect",
  severity: "medium",
  confidence: "firm",
  asset_type: "url",
  asset_value: "https://api-regression.example.com/redirect",
  vuln_status: "new",
  source_run_uuid: "bulk-triage-run"
}')
bulk_triage_resp=$(curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$bulk_triage_body")
bulk_triage_id=$(printf '%s' "$bulk_triage_resp" | jq -er '.data.id')

bulk_triage_action_body=$(jq -nc --arg ws "$WORKSPACE" --argjson id "$bulk_triage_id" '{
  action: "triage",
  workspace: $ws,
  ids: [$id],
  analyst_verdict: "confirmed",
  analyst_notes: "live regression triage"
}')
bulk_triage_action=$(curl -sf -X POST "$BASE_URL/vulnerabilities/bulk" -H 'Content-Type: application/json' -d "$bulk_triage_action_body")
bulk_triage_updated=$(printf '%s' "$bulk_triage_action" | jq -er '.summary.updated')
bulk_triage_status=$(curl -sf "$BASE_URL/vulnerabilities/$bulk_triage_id" | jq -er '.data.vuln_status')

bulk_retest_body=$(jq -nc --arg ws "$WORKSPACE" '{
  workspace: $ws,
  vuln_info: "xss",
  vuln_title: "Reflected XSS",
  severity: "medium",
  confidence: "firm",
  asset_type: "url",
  asset_value: "https://api-regression.example.com/search",
  vuln_status: "verified",
  source_run_uuid: "bulk-retest-run"
}')
bulk_retest_resp=$(curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$bulk_retest_body")
bulk_retest_id=$(printf '%s' "$bulk_retest_resp" | jq -er '.data.id')

bulk_retest_action_body=$(jq -nc --arg ws "$WORKSPACE" --argjson id "$bulk_retest_id" '{
  action: "retest",
  workspace: $ws,
  ids: [$id],
  module: "web-classic",
  priority: "critical",
  params: {
    recheck_mode: "focused"
  }
}')
bulk_retest_action=$(curl -sf -X POST "$BASE_URL/vulnerabilities/bulk" -H 'Content-Type: application/json' -d "$bulk_retest_action_body")
bulk_retest_queued=$(printf '%s' "$bulk_retest_action" | jq -er '.summary.queued')
bulk_retest_status=$(curl -sf "$BASE_URL/vulnerabilities/$bulk_retest_id" | jq -er '.data.retest_status')

verified_vuln_body=$(jq -nc --arg ws "$WORKSPACE" --arg target "$TARGET" '{
  workspace: $ws,
  vuln_info: "sql-injection",
  vuln_title: "SQL Injection",
  severity: "high",
  confidence: "certain",
  asset_type: "url",
  asset_value: $target,
  vuln_status: "verified",
  source_run_uuid: "baseline-run-1"
}')
verified_vuln_resp=$(curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$verified_vuln_body")
verified_vuln_id=$(printf '%s' "$verified_vuln_resp" | jq -er '.data.id')

campaign_body=$(jq -nc --arg target "$TARGET" --arg low_target "$LOW_TARGET" '{
  name: "regression-campaign",
  flow: "general",
  targets: [$target, $low_target],
  priority: "high",
  deep_scan_workflow: "web-analysis",
  deep_scan_workflow_kind: "flow",
  auto_deep_scan: true,
  high_risk_severities: ["critical", "high"]
}')
campaign_resp=$(curl -sf -X POST "$BASE_URL/campaigns" -H 'Content-Type: application/json' -d "$campaign_body")
campaign_id=$(printf '%s' "$campaign_resp" | jq -er '.campaign_id')

profile_save_body=$(jq -nc '{
  description: "ops handoff",
  filters: {
    statuses: ["queued"]
  },
  sort: {
    by: "target",
    order: "desc"
  },
  format: "json"
}')
profile_save_resp=$(curl -sf -X PUT "$BASE_URL/campaigns/$campaign_id/profiles/ops-handoff" -H 'Content-Type: application/json' -d "$profile_save_body")
profile_saved_name=$(printf '%s' "$profile_save_resp" | jq -er '.data.name')
profile_saved_format=$(printf '%s' "$profile_save_resp" | jq -er '.data.format')

profile_list_resp=$(curl -sf "$BASE_URL/campaigns/$campaign_id/profiles")
profile_list_count=$(printf '%s' "$profile_list_resp" | jq -er '.data | length')
profile_list_name=$(printf '%s' "$profile_list_resp" | jq -er '.data[0].name')

import_body=$(jq -nc --arg ws "$WORKSPACE" --arg target "$TARGET" --arg source "$BASE_DIR/attack-chain.json" '{
  workspace: $ws,
  target: $target,
  run_uuid: "run-attack-1",
  source_path: $source
}')
import_resp=$(curl -sf -X POST "$BASE_URL/attack-chains/import" -H 'Content-Type: application/json' -d "$import_body")
report_id=$(printf '%s' "$import_resp" | jq -er '.data.id')
linked_backfill=$(printf '%s' "$import_resp" | jq -er '.linked_vulnerability_count')

attack_chain_list=$(curl -sf "$BASE_URL/attack-chains?workspace=$WORKSPACE")
attack_chain_list_count=$(printf '%s' "$attack_chain_list" | jq -er '.data | length')

attack_chain_detail=$(curl -sf "$BASE_URL/attack-chains/$report_id?verified_only=true")
attack_chain_detail_count=$(printf '%s' "$attack_chain_detail" | jq -er '.data.chains | length')
attack_chain_queue_recommendation=$(printf '%s' "$attack_chain_detail" | jq -er '.data.chains[0].queue_recommendation')
attack_chain_execution_ready=$(printf '%s' "$attack_chain_detail" | jq -er '.data.chains[0].execution_ready')
attack_chain_linked_count=$(printf '%s' "$attack_chain_detail" | jq -er '.data.chains[0].linked_vulnerability_count')

queue_retest_body=$(jq -nc '{module: "retest-module", verified_only: true}')
queue_retest_resp=$(curl -sf -X POST "$BASE_URL/attack-chains/$report_id/queue-retest" -H 'Content-Type: application/json' -d "$queue_retest_body")
queue_retest_count=$(printf '%s' "$queue_retest_resp" | jq -er '.queued')

campaign_status=$(curl -sf "$BASE_URL/campaigns/$campaign_id")
campaign_high_risk=$(printf '%s' "$campaign_status" | jq -er --arg target "$TARGET" 'if (.high_risk_targets | index($target)) then "yes" else "no" end')
risk_level=$(printf '%s' "$campaign_status" | jq -er --arg target "$TARGET" '.targets[] | select(.target == $target) | .risk_level')
operational_hits=$(printf '%s' "$campaign_status" | jq -er --arg target "$TARGET" '.targets[] | select(.target == $target) | .attack_chain_summary.operational_hits')
verified_hits=$(printf '%s' "$campaign_status" | jq -er --arg target "$TARGET" '.targets[] | select(.target == $target) | .attack_chain_summary.verified_hits')

campaign_deep_resp=$(curl -sf -X POST "$BASE_URL/campaigns/$campaign_id/deep-scan")
campaign_deep_queued=$(printf '%s' "$campaign_deep_resp" | jq -er '.queued_count')

campaign_report=$(curl -sf "$BASE_URL/campaigns/$campaign_id/report")
report_high_risk_targets=$(printf '%s' "$campaign_report" | jq -er '.summary.high_risk_targets')
report_deep_configured=$(printf '%s' "$campaign_report" | jq -er '.deep_scan.configured')
report_deep_queued_targets=$(printf '%s' "$campaign_report" | jq -er '.deep_scan.queued_targets')
report_target_trigger=$(printf '%s' "$campaign_report" | jq -er --arg target "$TARGET" '.targets[] | select(.target == $target) | .latest_trigger_type')
report_target_deep_runs=$(printf '%s' "$campaign_report" | jq -er --arg target "$TARGET" '.targets[] | select(.target == $target) | .deep_scan_runs')
report_total_targets=$(printf '%s' "$campaign_report" | jq -er '.total_targets')
report_result_count=$(printf '%s' "$campaign_report" | jq -er '.result_count')

campaign_report_filtered=$(curl -sf "$BASE_URL/campaigns/$campaign_id/report?risk=high&status=queued&trigger=campaign&preset=high-risk")
report_filtered_result_count=$(printf '%s' "$campaign_report_filtered" | jq -er '.result_count')
report_filtered_target=$(printf '%s' "$campaign_report_filtered" | jq -er '.targets[0].target')
report_filtered_preset=$(printf '%s' "$campaign_report_filtered" | jq -er '.filters_applied.preset')

campaign_report_profile=$(curl -sf "$BASE_URL/campaigns/$campaign_id/report?profile=ops-handoff")
report_profile_applied=$(printf '%s' "$campaign_report_profile" | jq -er '.profile_applied')
report_profile_sort_by=$(printf '%s' "$campaign_report_profile" | jq -er '.sort_applied.by')
report_profile_sort_order=$(printf '%s' "$campaign_report_profile" | jq -er '.sort_applied.order')
report_profile_first_target=$(printf '%s' "$campaign_report_profile" | jq -er '.targets[0].target')

campaign_export_headers="$BASE_DIR/campaign-report.headers"
campaign_export_csv="$BASE_DIR/campaign-report.csv"
curl -sfD "$campaign_export_headers" "$BASE_URL/campaigns/$campaign_id/export" -o "$campaign_export_csv"
export_content_type=$(awk 'BEGIN{IGNORECASE=1} /^content-type:/ {sub(/\r$/, "", $0); print $0}' "$campaign_export_headers")
export_disposition=$(awk 'BEGIN{IGNORECASE=1} /^content-disposition:/ {sub(/\r$/, "", $0); print $0}' "$campaign_export_headers")
export_row_count=$(wc -l < "$campaign_export_csv" | tr -d ' ')
export_has_target=$(grep -c "$TARGET" "$campaign_export_csv")

campaign_export_profile=$(curl -sf "$BASE_URL/campaigns/$campaign_id/export?profile=ops-handoff")
export_profile_applied=$(printf '%s' "$campaign_export_profile" | jq -er '.profile_applied')
export_profile_sort_by=$(printf '%s' "$campaign_export_profile" | jq -er '.sort_applied.by')
export_profile_sort_order=$(printf '%s' "$campaign_export_profile" | jq -er '.sort_applied.order')

profile_delete_resp=$(curl -sf -X DELETE "$BASE_URL/campaigns/$campaign_id/profiles/ops-handoff")
profile_deleted=$(printf '%s' "$profile_delete_resp" | jq -er '.deleted')

vuln_detail=$(curl -sf "$BASE_URL/vulnerabilities/$verified_vuln_id")
attack_chain_ref=$(printf '%s' "$vuln_detail" | jq -er '.data.attack_chain_ref')
related_attack_chain_count=$(printf '%s' "$vuln_detail" | jq -er '.data.related_attack_chains | length')
retest_status=$(printf '%s' "$vuln_detail" | jq -er '.data.retest_status')
evidence_version=$(printf '%s' "$vuln_detail" | jq -er '.data.evidence_version')

assert_eq "$bulk_triage_updated" "1" "bulk triage updated count"
assert_eq "$bulk_triage_status" "triaged" "bulk triage status"
assert_eq "$bulk_retest_queued" "1" "bulk retest queued count"
assert_eq "$bulk_retest_status" "queued" "bulk retest status"
assert_eq "$linked_backfill" "1" "attack chain backfill count"
assert_eq "$attack_chain_list_count" "1" "attack chain list count"
assert_eq "$attack_chain_detail_count" "1" "attack chain detail count"
assert_eq "$attack_chain_queue_recommendation" "queue-retest" "attack chain queue recommendation"
assert_eq "$attack_chain_execution_ready" "true" "attack chain execution ready"
assert_eq "$attack_chain_linked_count" "1" "attack chain linked vulnerability count"
assert_eq "$queue_retest_count" "1" "attack chain queued retest count"
assert_eq "$profile_saved_name" "ops-handoff" "saved profile name"
assert_eq "$profile_saved_format" "json" "saved profile format"
assert_eq "$profile_list_count" "1" "profile list count"
assert_eq "$profile_list_name" "ops-handoff" "profile list name"
assert_eq "$campaign_high_risk" "yes" "campaign high risk target"
assert_eq "$risk_level" "high" "campaign target risk"
assert_eq "$operational_hits" "1" "campaign operational hits"
assert_eq "$verified_hits" "0" "campaign verified hits after retest queue"
assert_eq "$campaign_deep_queued" "1" "campaign deep scan queued count"
assert_eq "$report_high_risk_targets" "1" "campaign report high risk target count"
assert_eq "$report_deep_configured" "true" "campaign report deep scan configured"
assert_eq "$report_deep_queued_targets" "1" "campaign report deep scan queued targets"
assert_eq "$report_target_trigger" "campaign" "campaign report target trigger"
assert_eq "$report_target_deep_runs" "1" "campaign report deep scan runs"
assert_eq "$report_total_targets" "2" "campaign report total targets"
assert_eq "$report_result_count" "2" "campaign report result count"
assert_eq "$report_filtered_result_count" "1" "filtered report result count"
assert_eq "$report_filtered_target" "$TARGET" "filtered report target"
assert_eq "$report_filtered_preset" "high-risk" "filtered report preset"
assert_eq "$report_profile_applied" "ops-handoff" "report profile applied"
assert_eq "$report_profile_sort_by" "target" "report profile sort field"
assert_eq "$report_profile_sort_order" "desc" "report profile sort order"
assert_eq "$report_profile_first_target" "$TARGET" "report profile first target"
assert_contains "$export_content_type" "text/csv" "campaign export content type"
assert_contains "$export_disposition" "campaign-${campaign_id}-report.csv" "campaign export disposition"
assert_eq "$export_row_count" "3" "campaign export row count"
assert_eq "$export_has_target" "1" "campaign export target count"
assert_eq "$export_profile_applied" "ops-handoff" "export profile applied"
assert_eq "$export_profile_sort_by" "target" "export profile sort field"
assert_eq "$export_profile_sort_order" "desc" "export profile sort order"
assert_eq "$profile_deleted" "true" "profile delete response"
assert_contains "$attack_chain_ref" "report:" "vulnerability attack chain ref prefix"
assert_contains "$attack_chain_ref" ":chain-login" "vulnerability attack chain ref chain id"
assert_eq "$related_attack_chain_count" "1" "vulnerability related attack chains"
assert_eq "$retest_status" "queued" "vulnerability retest status"
if (( evidence_version < 3 )); then
  echo "assertion failed for vulnerability evidence_version: expected >= 3, got '${evidence_version}'" >&2
  exit 1
fi

printf 'ok live api regression: campaign, vulnerability, attack-chain\n'
