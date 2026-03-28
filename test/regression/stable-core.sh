#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
MAIN_WORKFLOW_DIR="${MAIN_WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
QUEUE_WORKFLOW_DIR="${QUEUE_WORKFLOW_DIR:-${ROOT_DIR}/test/regression/workflows/queue-live}"
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
validate_workflow_path "${MAIN_WORKFLOW_DIR}/fragments/do-ai-knowledge-autolearn.yaml"

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

BASE_DIR=/tmp/osm-stable-core-queue \
PORT=8908 \
OSMEDEUS_BIN="$OSMEDEUS_BIN" \
WORKFLOW_DIR="$QUEUE_WORKFLOW_DIR" \
bash "${ROOT_DIR}/test/regression/queue-runner-live.sh"

printf 'ok stable core regression: workflows validated and live regressions passed\n'
