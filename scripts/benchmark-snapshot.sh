#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MOLSTAR_BIN="${MOLSTAR_BIN:-$ROOT/bin/molstar}"
OUT_ROOT="${OUT_ROOT:-$ROOT/outputs/benchmarks}"
ITERATIONS="${ITERATIONS:-3}"
WARMUP="${WARMUP:-1}"
SIZE="${SIZE:-256x256}"
RENDERER_MODE="${RENDERER_MODE:-auto}"
LABEL="${LABEL:-local}"
BASELINE="${BASELINE:-}"
MAX_REGRESSION_PERCENT="${MAX_REGRESSION_PERCENT:-25}"

if [ ! -x "$MOLSTAR_BIN" ]; then
  LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
  (cd "$ROOT" && go build -ldflags "$LDFLAGS" -o "$MOLSTAR_BIN" ./cmd/molstar)
fi

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="$OUT_ROOT/$timestamp"
mkdir -p "$run_dir"

args=(
  bench
  --iterations "$ITERATIONS"
  --warmup "$WARMUP"
  --size "$SIZE"
  --renderer-mode "$RENDERER_MODE"
  --label "$LABEL"
  --out-dir "$run_dir/artifacts"
  --report "$run_dir/bench.json"
  --json
)
if [ -n "$BASELINE" ]; then
  args+=(--baseline "$BASELINE" --max-regression-percent "$MAX_REGRESSION_PERCENT")
fi

"$MOLSTAR_BIN" "${args[@]}" | tee "$run_dir/stdout.json" >/dev/null
printf '%s\n' "$run_dir/bench.json"
