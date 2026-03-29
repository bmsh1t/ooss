#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-scan-content-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"

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
  if ! grep -Fq -- "$needle" "$file"; then
    echo "assertion failed for ${label}: expected '$needle' in $file" >&2
    exit 1
  fi
}

assert_missing_or_empty() {
  local file="$1"
  local label="$2"
  if [[ -f "$file" ]] && [[ -s "$file" ]]; then
    echo "assertion failed for ${label}: expected missing or empty file, got $file" >&2
    exit 1
  fi
}

dump_logs() {
  local file
  for file in \
    "$BASE_DIR/validate.log" \
    "$BASE_DIR/run-success.log" \
    "$BASE_DIR/run-fallback.log" \
    "$BASE_DIR/mock-deparos-success.log" \
    "$BASE_DIR/mock-deparos-fallback.log" \
    "$BASE_DIR/mock-ffuf-success.log" \
    "$BASE_DIR/mock-ffuf-fallback.log" \
    "$BASE_DIR/mock-httpx-success.log" \
    "$BASE_DIR/mock-httpx-fallback.log"; do
    if [[ -f "$file" ]]; then
      echo "---- $(basename "$file") ----" >&2
      tail -n 200 "$file" >&2 || true
    fi
  done
}

trap 'rc=$?; if [[ $rc -ne 0 ]]; then dump_logs; fi' EXIT

require_cmd jq
require_cmd timeout

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

MOCK_BIN="$BASE_DIR/mockbin"
mkdir -p "$MOCK_BIN"

cat >"$MOCK_BIN/deparos" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

LOG_FILE="${MOCK_DEPAROS_LOG:-}"
if [[ -n "$LOG_FILE" ]]; then
  printf '%s\n' "$*" >>"$LOG_FILE"
fi

SUBCOMMAND="${1:-}"
shift || true

case "$SUBCOMMAND" in
  discover)
    DB_PATH=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --db)
          DB_PATH="${2:-}"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    if [[ "${MOCK_DEPAROS_MODE:-success}" == "success" ]] && [[ -n "$DB_PATH" ]]; then
      mkdir -p "$(dirname "$DB_PATH")"
      : >"$DB_PATH"
    fi
    ;;
  export)
    DB_PATH=""
    FORMAT=""
    OUTPUT=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --db)
          DB_PATH="${2:-}"
          shift 2
          ;;
        -f)
          FORMAT="${2:-}"
          shift 2
          ;;
        -o)
          OUTPUT="${2:-}"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    [[ -n "$DB_PATH" && -f "$DB_PATH" ]] || exit 1
    [[ -n "$OUTPUT" ]] || exit 1
    mkdir -p "$(dirname "$OUTPUT")"
    case "$FORMAT" in
      html)
        cat >"$OUTPUT" <<HTML
<html><body><h1>Mock Deparos Report</h1></body></html>
HTML
        ;;
      jsonl)
        printf '%s\n' "{\"url\":\"${MOCK_DEPAROS_URL}\",\"status_code\":200,\"words\":12,\"webserver\":\"mock-deparos\",\"content_length\":128,\"content_type\":\"text/html\",\"location\":\"\"}" >"$OUTPUT"
        ;;
      *)
        exit 1
        ;;
    esac
    ;;
  *)
    exit 0
    ;;
esac
EOF
chmod +x "$MOCK_BIN/deparos"

cat >"$MOCK_BIN/ffuf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

LOG_FILE="${MOCK_FFUF_LOG:-}"
if [[ -n "$LOG_FILE" ]]; then
  printf '%s\n' "$*" >>"$LOG_FILE"
fi

printf '%s\n' "{\"url\":\"${MOCK_FFUF_URL}\",\"status_code\":200,\"words\":8,\"content_length\":64,\"content_type\":\"text/html\",\"location\":\"\"}"
EOF
chmod +x "$MOCK_BIN/ffuf"

cat >"$MOCK_BIN/httpx" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

LOG_FILE="${MOCK_HTTPX_LOG:-}"
INPUT=""
OUTPUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -l)
      INPUT="${2:-}"
      shift 2
      ;;
    -o)
      OUTPUT="${2:-}"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

[[ -n "$INPUT" && -f "$INPUT" ]] || exit 1
[[ -n "$OUTPUT" ]] || exit 1

if [[ -n "$LOG_FILE" ]]; then
  printf 'input=%s output=%s\n' "$INPUT" "$OUTPUT" >>"$LOG_FILE"
fi

mkdir -p "$(dirname "$OUTPUT")"
: >"$OUTPUT"
while IFS= read -r url; do
  [[ -n "$url" ]] || continue
  escaped_url=$(printf '%s' "$url" | jq -R '.')
  printf '{"url":%s,"title":"Mock Fingerprint","status_code":200,"content_length":256,"lines":10,"webserver":"mock-httpx","tech":["mock-tech"],"cdn":false}\n' "$escaped_url" >>"$OUTPUT"
done <"$INPUT"
EOF
chmod +x "$MOCK_BIN/httpx"

OSM_SKIP_PATH_SETUP=1 \
OSM_WORKSPACES="$WORKSPACES_DIR" \
PATH="$MOCK_BIN:$PATH" \
"$OSMEDEUS_BIN" \
  --base-folder "$BASE_DIR" \
  --workflow-folder "$WORKFLOW_DIR" \
  workflow validate scan-content \
  >"$BASE_DIR/validate.log" 2>&1

run_case() {
  local case_name="$1"
  local deparos_mode="$2"
  local target="$3"
  local expected_url="$4"

  local workspace_dir="$WORKSPACES_DIR/$target"
  local http_file="$workspace_dir/probing/http-$target.txt"
  local content_dir="$workspace_dir/content-discovery"
  local url_file="$content_dir/content-discovery-url-$target.txt"
  local fingerprint_file="$content_dir/content-fingerprint-$target.jsonl"

  mkdir -p "$workspace_dir/probing"
  cat >"$http_file" <<EOF
http://127.0.0.1/mock-$case_name
EOF

  local ffuf_log="$BASE_DIR/mock-ffuf-$case_name.log"

  OSM_SKIP_PATH_SETUP=1 \
  OSM_WORKSPACES="$WORKSPACES_DIR" \
  PATH="$MOCK_BIN:$PATH" \
  MOCK_DEPAROS_MODE="$deparos_mode" \
  MOCK_DEPAROS_URL="http://127.0.0.1/mock-$case_name/deparos-hit" \
  MOCK_FFUF_URL="http://127.0.0.1/mock-$case_name/ffuf-hit" \
  MOCK_DEPAROS_LOG="$BASE_DIR/mock-deparos-$case_name.log" \
  MOCK_FFUF_LOG="$ffuf_log" \
  MOCK_HTTPX_LOG="$BASE_DIR/mock-httpx-$case_name.log" \
  "$OSMEDEUS_BIN" \
    --base-folder "$BASE_DIR" \
    --workflow-folder "$WORKFLOW_DIR" \
    run \
    -m scan-content \
    -t "$target" \
    -p "httpFile=$http_file" \
    -p "deparosParallel=1" \
    -p "ffufParallel=1" \
    -p "httpThreads=2" \
    -p "deparosSessionName=scan-content-$case_name" \
    >"$BASE_DIR/run-$case_name.log" 2>&1

  assert_file_contains "$url_file" "$expected_url" "$case_name discovered urls"
  assert_file_contains "$fingerprint_file" "$expected_url" "$case_name fingerprint output"

  if [[ "$case_name" == "success" ]]; then
    assert_file_contains "$content_dir/deparos-$target.html" "Mock Deparos Report" "$case_name deparos report"
    assert_missing_or_empty "$ffuf_log" "$case_name ffuf should be skipped"
  else
    assert_file_contains "$ffuf_log" "-json" "$case_name ffuf invocation"
  fi
}

run_case "success" "success" "scan-content-success.local" "http://127.0.0.1/mock-success/deparos-hit"
run_case "fallback" "empty" "scan-content-fallback.local" "http://127.0.0.1/mock-fallback/ffuf-hit"

echo "ok scan-content smoke regression: deparos success skips ffuf and fallback ffuf still fingerprints discovered URLs"
