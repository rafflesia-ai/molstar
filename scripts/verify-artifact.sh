#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/verify-artifact-lib.sh"

ARTIFACT_PATH="$(resolve_artifact "${1:-}")"
WORKDIR="$(mktemp -d)"
TMP_ARTIFACT="$WORKDIR/$(basename "$ARTIFACT_PATH")"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

cp "$ARTIFACT_PATH" "$TMP_ARTIFACT"
verify_artifact_contents "$TMP_ARTIFACT" "$WORKDIR/archive-list.txt"
ARTIFACT="$TMP_ARTIFACT" "$ROOT/scripts/verify-artifact-molstar.sh"

printf 'verified artifact %s\n' "$ARTIFACT_PATH"
