#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-queue-live}"
PORT="${PORT:-8908}"
BASE_URL="http://127.0.0.1:${PORT}/osm/api"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/queue-live}"
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

urlencode() {
  jq -rn --arg value "$1" '$value|@uri'
}

wait_for_server() {
  local health_url="http://127.0.0.1:${PORT}/health"
  for _ in $(seq 1 80); do
    if curl -sf "$health_url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  echo "server did not become healthy: ${health_url}" >&2
  exit 1
}

wait_for_json_value() {
  local url="$1"
  local jq_expr="$2"
  local expected="$3"
  local label="$4"
  local actual=""
  for _ in $(seq 1 160); do
    actual=$(curl -sf "$url" 2>/dev/null | jq -er "$jq_expr" 2>/dev/null || true)
    if [[ "$actual" == "$expected" ]]; then
      return 0
    fi
    sleep 0.5
  done
  echo "timed out waiting for ${label}: expected '${expected}', got '${actual}'" >&2
  exit 1
}

dump_logs() {
  if [[ -f "$BASE_DIR/server.log" ]]; then
    echo "---- server.log ----" >&2
    tail -n 120 "$BASE_DIR/server.log" >&2 || true
  fi
  if [[ -f "$BASE_DIR/worker.log" ]]; then
    echo "---- worker.log ----" >&2
    tail -n 120 "$BASE_DIR/worker.log" >&2 || true
  fi
}

cleanup() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    dump_logs
  fi
  if [[ -n "${WORKER_PID:-}" ]] && kill -0 "$WORKER_PID" >/dev/null 2>&1; then
    kill "$WORKER_PID" >/dev/null 2>&1 || true
    wait "$WORKER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd curl
require_cmd jq

if [[ ! -d "$WORKFLOW_DIR" ]]; then
  echo "workflow directory not found: $WORKFLOW_DIR" >&2
  exit 1
fi

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

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  worker queue run \
  --concurrency 1 \
  >"$BASE_DIR/worker.log" 2>&1 &
WORKER_PID=$!

sleep 1

CLI_TARGET="https://queue-cli.example.com/login"
CLI_WORKSPACE="queue-cli-space"

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  worker queue new \
  -f queue-smoke-flow \
  -t "$CLI_TARGET" \
  -p "space_name=$CLI_WORKSPACE" \
  >"$BASE_DIR/queue-new.log" 2>&1

cli_target_encoded=$(urlencode "$CLI_TARGET")
cli_runs_url="$BASE_URL/runs?workflow=queue-smoke-flow&target=${cli_target_encoded}"
wait_for_json_value "$cli_runs_url" '.data[0].status' "completed" "CLI queued run completion"
cli_workspace=$(curl -sf "$cli_runs_url" | jq -er '.data[0].workspace')
cli_trigger=$(curl -sf "$cli_runs_url" | jq -er '.data[0].trigger_type')
cli_marker=$(cat "$WORKSPACES_DIR/$CLI_WORKSPACE/regression/queue-smoke.json")

assert_eq "$cli_workspace" "$CLI_WORKSPACE" "CLI queued run workspace"
assert_eq "$cli_trigger" "cli" "CLI queued run trigger type"
assert_contains "$cli_marker" "$CLI_TARGET" "CLI queued run marker"

RETEST_TARGET="https://queue-retest.example.com/search"
RETEST_WORKSPACE="queue-retest-space"

retest_vuln_body=$(jq -nc --arg ws "$RETEST_WORKSPACE" --arg target "$RETEST_TARGET" '{
  workspace: $ws,
  vuln_info: "reflected-xss",
  vuln_title: "Reflected XSS",
  severity: "medium",
  confidence: "firm",
  asset_type: "url",
  asset_value: $target,
  vuln_status: "verified",
  source_run_uuid: "queue-retest-baseline"
}')
retest_vuln_resp=$(curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$retest_vuln_body")
retest_vuln_id=$(printf '%s' "$retest_vuln_resp" | jq -er '.data.id')

retest_queue_body=$(jq -nc '{module: "queue-retest-module"}')
retest_queue_resp=$(curl -sf -X POST "$BASE_URL/vulnerabilities/$retest_vuln_id/retest" -H 'Content-Type: application/json' -d "$retest_queue_body")
retest_run_uuid=$(printf '%s' "$retest_queue_resp" | jq -er '.run_uuid')

wait_for_json_value "$BASE_URL/runs/$retest_run_uuid" '.data.status' "completed" "retest queued run completion"
wait_for_json_value "$BASE_URL/vulnerabilities/$retest_vuln_id" '.data.retest_status' "completed" "retest vulnerability status"
wait_for_json_value "$BASE_URL/vulnerabilities/$retest_vuln_id" '.data.vuln_status' "triaged" "retest vulnerability lifecycle"
retest_marker=$(cat "$WORKSPACES_DIR/$RETEST_WORKSPACE/regression/queue-retest.json")
assert_contains "$retest_marker" "$RETEST_TARGET" "retest queued run marker"

CAMPAIGN_TARGET="https://queue-campaign.example.com"
CAMPAIGN_WORKSPACE="queue-campaign-space"

campaign_vuln_body=$(jq -nc --arg ws "$CAMPAIGN_WORKSPACE" --arg asset "${CAMPAIGN_TARGET}/admin" '{
  workspace: $ws,
  vuln_info: "admin-exposure",
  vuln_title: "Admin Exposure",
  severity: "high",
  confidence: "certain",
  asset_type: "url",
  asset_value: $asset,
  vuln_status: "verified",
  source_run_uuid: "queue-campaign-baseline"
}')
curl -sf -X POST "$BASE_URL/vulnerabilities" -H 'Content-Type: application/json' -d "$campaign_vuln_body" >/dev/null

campaign_body=$(jq -nc --arg target "$CAMPAIGN_TARGET" --arg ws "$CAMPAIGN_WORKSPACE" '{
  name: "queue-live-campaign",
  flow: "queue-smoke-flow",
  targets: [$target],
  params: {
    space_name: $ws
  },
  priority: "high",
  deep_scan_workflow: "queue-deep-module",
  deep_scan_workflow_kind: "module",
  auto_deep_scan: true,
  high_risk_severities: ["high"]
}')
campaign_resp=$(curl -sf -X POST "$BASE_URL/campaigns" -H 'Content-Type: application/json' -d "$campaign_body")
campaign_id=$(printf '%s' "$campaign_resp" | jq -er '.campaign_id')

campaign_status_url="$BASE_URL/campaigns/$campaign_id"
campaign_report_url="$BASE_URL/campaigns/$campaign_id/report"
campaign_target_encoded=$(urlencode "$CAMPAIGN_TARGET")
campaign_deep_runs_url="$BASE_URL/runs?workflow=queue-deep-module&target=${campaign_target_encoded}"

wait_for_json_value "$campaign_status_url" '.runs | map(select(.trigger_type == "campaign" and .status == "completed")) | length' "1" "campaign primary run completion"
wait_for_json_value "$campaign_deep_runs_url" '.data[0].status' "completed" "campaign deep-scan completion"
wait_for_json_value "$campaign_report_url" '.deep_scan.completed_runs' "1" "campaign deep-scan report completion"

campaign_high_risk=$(curl -sf "$campaign_status_url" | jq -er --arg target "$CAMPAIGN_TARGET" 'if (.high_risk_targets | index($target)) then "yes" else "no" end')
campaign_risk_level=$(curl -sf "$campaign_status_url" | jq -er --arg target "$CAMPAIGN_TARGET" '.targets[] | select(.target == $target) | .risk_level')
campaign_deep_trigger=$(curl -sf "$campaign_deep_runs_url" | jq -er '.data[0].trigger_type')
campaign_deep_total=$(curl -sf "$campaign_report_url" | jq -er '.deep_scan.total_runs')
campaign_deep_completed=$(curl -sf "$campaign_report_url" | jq -er '.deep_scan.completed_runs')
campaign_report_deep_trigger_count=$(curl -sf "$campaign_report_url" | jq -er '.trigger_distribution["campaign-deep-scan"]')
campaign_target_deep_runs=$(curl -sf "$campaign_report_url" | jq -er --arg target "$CAMPAIGN_TARGET" '.targets[] | select(.target == $target) | .deep_scan_runs')
deep_marker=$(cat "$WORKSPACES_DIR/$CAMPAIGN_WORKSPACE/regression/queue-deep.json")

assert_eq "$campaign_high_risk" "yes" "campaign high-risk target"
assert_eq "$campaign_risk_level" "high" "campaign target risk"
assert_eq "$campaign_deep_trigger" "campaign-deep-scan" "campaign deep-scan run trigger type"
assert_eq "$campaign_deep_total" "1" "campaign deep-scan total runs"
assert_eq "$campaign_deep_completed" "1" "campaign deep-scan completed runs"
assert_eq "$campaign_report_deep_trigger_count" "1" "campaign report deep-scan trigger count"
assert_eq "$campaign_target_deep_runs" "1" "campaign target deep-scan runs"
assert_contains "$deep_marker" "$CAMPAIGN_TARGET" "campaign deep-scan marker"

printf 'ok live queue regression: cli queue, vuln retest, campaign deep-scan\n'
