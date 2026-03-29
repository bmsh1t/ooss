#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-vuln-suite-nuclei-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
HTTP_PORT="${HTTP_PORT:-18941}"
TARGET="${TARGET:-nuclei-smoke.local}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

assert_file_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  if [[ ! -f "$file" ]]; then
    echo "missing expected file for ${label}: $file" >&2
    exit 1
  fi
  if ! grep -Fq "$needle" "$file"; then
    echo "assertion failed for ${label}: expected '$needle' in $file" >&2
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

dump_logs() {
  for file in "$BASE_DIR/http.log" "$BASE_DIR/validate.log" "$BASE_DIR/run.log"; do
    if [[ -f "$file" ]]; then
      echo "---- $(basename "$file") ----" >&2
      tail -n 200 "$file" >&2 || true
    fi
  done
}

cleanup() {
  if [[ -n "${HTTP_PID:-}" ]] && kill -0 "$HTTP_PID" >/dev/null 2>&1; then
    kill "$HTTP_PID" >/dev/null 2>&1 || true
    wait "$HTTP_PID" >/dev/null 2>&1 || true
  fi
}
trap 'rc=$?; cleanup; if [[ $rc -ne 0 ]]; then dump_logs; fi' EXIT

require_cmd curl
require_cmd python3

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

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
server:
  enabled_auth_api: false
EOF

WEB_ROOT="$BASE_DIR/webroot"
mkdir -p "$WEB_ROOT" "$BASE_DIR/templates"

cat >"$WEB_ROOT/index.html" <<'EOF'
<html>
  <head><title>Workflow Nuclei Smoke</title></head>
  <body>workflow nuclei smoke marker</body>
</html>
EOF

TEMPLATE_FILE="$BASE_DIR/templates/workflow-nuclei-smoke.yaml"
cat >"$TEMPLATE_FILE" <<'EOF'
id: workflow-nuclei-smoke

info:
  name: Workflow Nuclei Smoke
  author: osmedeus
  severity: info

http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: word
        part: body
        words:
          - "workflow nuclei smoke marker"
EOF

python3 -m http.server "$HTTP_PORT" --bind 127.0.0.1 --directory "$WEB_ROOT" \
  >"$BASE_DIR/http.log" 2>&1 &
HTTP_PID=$!
wait_for_url "http://127.0.0.1:${HTTP_PORT}/" "local nuclei smoke server"

WORKSPACE_SEED_DIR="$WORKSPACES_DIR/$TARGET"
mkdir -p "$WORKSPACE_SEED_DIR/probing" "$WORKSPACE_SEED_DIR/vulnscan"

HTTP_FILE="$WORKSPACE_SEED_DIR/probing/http-$TARGET.txt"
cat >"$HTTP_FILE" <<EOF
http://127.0.0.1:${HTTP_PORT}
EOF

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate 09-vuln-suite \
  >"$BASE_DIR/validate.log" 2>&1

run_args=(
  --base-folder "$BASE_DIR"
  --workflow-folder "$WORKFLOW_DIR"
  run
  -m 09-vuln-suite
  -t "$TARGET"
  -p "httpFile=$HTTP_FILE"
  -p "nucleiTemplateConfig=$TEMPLATE_FILE"
  -p "nucleiSeverity=info"
  -p "nucleiTimeout=2m"
  -p "nucleiThreads=2"
  -p "nucleiRateLimit=5"
  -p "enableSmartTemplateSelection=false"
  -p "enableWafAwareScan=false"
  -p "enableUserAgentRotation=false"
  -p "enableProxyRotation=false"
  -p "enableAssetPrioritization=false"
  -p "enableJitter=false"
  -p "enableOsintFeedback=false"
  -p "enableLlmAnalysis=false"
  -p "enableLlmWafBypass=false"
  -p "enableNucleiDast=false"
  -p "enableSsrfScan=false"
  -p "enableSstiScan=false"
  -p "enableCrlfScan=false"
  -p "enableCommScan=false"
  -p "enableSqliScan=false"
  -p "enableLfiScan=false"
  -p "enableTakeoverScan=false"
  -p "enableSecretScan=false"
  -p "enableSmugglingScan=false"
  -p "enableWebcacheScan=false"
  -p "enable4xxBypassScan=false"
  -p "enableFrayScan=false"
  -p "enableCommandInjection=false"
  -p "enableTestSSL=false"
  -p "enableSpraying=false"
)

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
"$OSMEDEUS_BIN" \
  "${run_args[@]}" \
  >"$BASE_DIR/run.log" 2>&1

NUCLEI_JSONL="$WORKSPACE_SEED_DIR/vulnscan/nuclei-jsonl-$TARGET.txt"
NUCLEI_REPORT="$WORKSPACE_SEED_DIR/vulnscan/nuclei-overview-report-$TARGET.md"

assert_file_contains "$NUCLEI_JSONL" "workflow-nuclei-smoke" "nuclei jsonl output"
assert_file_contains "$NUCLEI_REPORT" "Workflow Nuclei Smoke" "nuclei markdown report"

echo "ok vuln-suite nuclei smoke regression: workflow Nuclei path produces a deterministic local match"
