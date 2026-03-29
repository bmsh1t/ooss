#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
MAIN_WORKFLOW_DIR="${MAIN_WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
QUEUE_WORKFLOW_DIR="${QUEUE_WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/queue-live}"
AI_SMOKE_WORKFLOW_DIR="${AI_SMOKE_WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/ai-smoke}"
AI_SEMANTIC_WORKFLOW_DIR="${AI_SEMANTIC_WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/ai-semantic}"
VALIDATE_BASE="${VALIDATE_BASE:-/tmp/osm-stable-core-validate}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require_cmd bash

if [[ ! -x "$OSMEDEUS_BIN" ]]; then
  echo "osmedeus binary not found or not executable: $OSMEDEUS_BIN" >&2
  exit 1
fi

validate_workflow() {
  local workflow="$1"
  "$OSMEDEUS_BIN" \
    --base-folder "$VALIDATE_BASE" \
    --workflow-folder "$MAIN_WORKFLOW_DIR" \
    workflow validate "$workflow"
}

validate_workflow_path() {
  local workflow_path="$1"
  "$OSMEDEUS_BIN" \
    --base-folder "$VALIDATE_BASE" \
    --workflow-folder "$MAIN_WORKFLOW_DIR" \
    workflow validate "$workflow_path"
}

validate_workflow "superdomain-extensive-ai-stable"
validate_workflow "superdomain-extensive-ai-hybrid"
validate_workflow "superdomain-extensive-ai-optimized"
validate_workflow "superdomain-extensive-ai-lite"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/00-incremental-check.yaml"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/01-osint.yaml"
validate_workflow "scan-content"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/scan-backup.yaml"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/scan-vuln.yaml"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/url-gf.yaml"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/common/iis-shortname.yaml"
validate_workflow "04-http-probe"
validate_workflow "05-fingerprint"
validate_workflow "06-web-crawl"
validate_workflow "07-content-analysis"
validate_workflow "09-vuln-suite"
validate_workflow "10-report"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/fragments/do-ai-knowledge-autolearn.yaml"
validate_workflow_path "${MAIN_WORKFLOW_DIR}/fragments/do-scan-content.yaml"

BASE_DIR=/tmp/osm-stable-core-ai-workflow \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$AI_SMOKE_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/ai-workflow-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-ai-semantic \
EMBED_PORT=8911 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$AI_SEMANTIC_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/ai-semantic-vector-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-superdomain-lite \
EMBED_PORT=8913 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/superdomain-lite-flow-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-superdomain-stable \
EMBED_PORT=8914 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/superdomain-stable-flow-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-scan-content \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/scan-content-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-vuln-suite \
HTTP_PORT=18942 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/vuln-suite-nuclei-smoke.sh"

BASE_DIR=/tmp/osm-stable-core-ai \
PORT=8906 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/api-ai-workbench-live.sh"

BASE_DIR=/tmp/osm-stable-core-knowledge \
PORT=8907 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/api-knowledge-live.sh"

BASE_DIR=/tmp/osm-stable-core-vector \
PORT=8909 \
EMBED_PORT=8910 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$MAIN_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/api-vector-knowledge-live.sh"

BASE_DIR=/tmp/osm-stable-core-queue \
PORT=8908 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$QUEUE_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/queue-runner-live.sh"

printf 'ok stable core regression: workflows validated and live regressions passed\n'
