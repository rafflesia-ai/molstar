#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${IMAGE:-headlessmolstar:local}"
DOCKER_BIN="${DOCKER_BIN:-docker}"
DOGFOOD_DOCKER_BUILD="${DOGFOOD_DOCKER_BUILD:-1}"
DOGFOOD_DOCKER_RENDER="${DOGFOOD_DOCKER_RENDER:-auto}"
DOGFOOD_DOCKER_GPU="${DOGFOOD_DOCKER_GPU:-0}"

if [ -n "${DOGFOOD_DOCKER_OUT_DIR:-}" ]; then
  OUT_DIR="$DOGFOOD_DOCKER_OUT_DIR"
  mkdir -p "$OUT_DIR"
else
  OUT_DIR="$(mktemp -d)"
fi

cleanup() {
  local status="$?"
  if [ "${DOGFOOD_DOCKER_KEEP:-0}" = "1" ] || [ -n "${DOGFOOD_DOCKER_OUT_DIR:-}" ]; then
    echo "docker dogfood artifacts kept at $OUT_DIR" >&2
  else
    rm -rf "$OUT_DIR"
  fi
  exit "$status"
}
trap cleanup EXIT

cd "$ROOT"

if [ "$DOGFOOD_DOCKER_BUILD" = "1" ]; then
  "$DOCKER_BIN" build -t "$IMAGE" .
fi

run_profile() {
  local profile="$1"
  local out="$2"
  mkdir -p "$out"
  DOCKER_VERIFY_PROFILE="$profile" \
    DOCKER_VERIFY_OUT_DIR="$out" \
    IMAGE="$IMAGE" \
    DOCKER_BIN="$DOCKER_BIN" \
    ./scripts/verify-docker.sh
}

run_profile runtime "$OUT_DIR/runtime"

case "$DOGFOOD_DOCKER_RENDER" in
  0|false|no|none)
    ;;
  1|true|yes|render)
    run_profile render "$OUT_DIR/render"
    ;;
  auto)
    run_profile auto "$OUT_DIR/auto-render"
    ;;
  *)
    echo "DOGFOOD_DOCKER_RENDER must be auto, render, true, false, or none" >&2
    exit 2
    ;;
esac

if [ "$DOGFOOD_DOCKER_GPU" = "1" ]; then
  DOCKER_RUN_EXTRA_ARGS="${DOCKER_RUN_EXTRA_ARGS:---gpus all}" \
    DOCKER_VERIFY_PROFILE=render \
    DOCKER_VERIFY_OUT_DIR="$OUT_DIR/gpu-render" \
    IMAGE="$IMAGE" \
    DOCKER_BIN="$DOCKER_BIN" \
    ./scripts/verify-docker.sh
fi

python3 - "$OUT_DIR" "$IMAGE" "$DOGFOOD_DOCKER_RENDER" "$DOGFOOD_DOCKER_GPU" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
image = sys.argv[2]
render = sys.argv[3]
gpu = sys.argv[4] == "1"
reports = {}
for path in sorted(root.glob("*/docker-smoke-summary.json")):
    with path.open() as f:
        reports[path.parent.name] = json.load(f)
ok = bool(reports) and all(report.get("ok") for report in reports.values())
print(json.dumps({
    "ok": ok,
    "image": image,
    "render": render,
    "gpu": gpu,
    "reports": reports,
}, indent=2))
raise SystemExit(0 if ok else 1)
PY
