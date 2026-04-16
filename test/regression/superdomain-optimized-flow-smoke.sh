#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BASE_DIR="${BASE_DIR:-/tmp/osm-superdomain-optimized-flow-smoke}"
WORKFLOW_DIR="${WORKFLOW_DIR:-${ROOT_DIR}/osmedeus-base/workflows}"
OSMEDEUS_BIN="${OSMEDEUS_BIN:-${ROOT_DIR}/build/bin/osmedeus}"
WORKSPACES_DIR="${WORKSPACES_DIR:-${BASE_DIR}/workspaces}"
EMBED_PORT="${EMBED_PORT:-8915}"
TARGET="${TARGET:-example.com}"
KNOWLEDGE_WORKSPACE="${KNOWLEDGE_WORKSPACE:-example.com}"
SHARED_WORKSPACE="${SHARED_WORKSPACE:-shared-web}"
GLOBAL_WORKSPACE="${GLOBAL_WORKSPACE:-global}"
MOCK_PROVIDER="${MOCK_PROVIDER:-mock-openai}"
EMBED_MODEL="${EMBED_MODEL:-test-embedding-3-small}"
MOCK_SERVER_SCRIPT="${MOCK_SERVER_SCRIPT:-${ROOT_DIR}/test/regression/mock-embedding-server.py}"
FLOW_NAME="${FLOW_NAME:-superdomain-extensive-ai-optimized}"
FLOW_LABEL="${FLOW_LABEL:-optimized}"
FLOW_SUCCESS_MESSAGE="${FLOW_SUCCESS_MESSAGE:-current-source optimized flow executes the full AI closure and knowledge autolearn path}"

exec env \
  ROOT_DIR="$ROOT_DIR" \
  BASE_DIR="$BASE_DIR" \
  WORKFLOW_DIR="$WORKFLOW_DIR" \
  OSMEDEUS_BIN="$OSMEDEUS_BIN" \
  WORKSPACES_DIR="$WORKSPACES_DIR" \
  EMBED_PORT="$EMBED_PORT" \
  TARGET="$TARGET" \
  KNOWLEDGE_WORKSPACE="$KNOWLEDGE_WORKSPACE" \
  SHARED_WORKSPACE="$SHARED_WORKSPACE" \
  GLOBAL_WORKSPACE="$GLOBAL_WORKSPACE" \
  MOCK_PROVIDER="$MOCK_PROVIDER" \
  EMBED_MODEL="$EMBED_MODEL" \
  MOCK_SERVER_SCRIPT="$MOCK_SERVER_SCRIPT" \
  FLOW_NAME="$FLOW_NAME" \
  FLOW_LABEL="$FLOW_LABEL" \
  FLOW_SUCCESS_MESSAGE="$FLOW_SUCCESS_MESSAGE" \
  bash "$ROOT_DIR/test/regression/superdomain-stable-flow-smoke.sh"
