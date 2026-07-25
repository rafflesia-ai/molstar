#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEEP="${DOGFOOD_KEEP:-0}"
MOLSTAR_BIN="${MOLSTAR_BIN:-$ROOT/bin/molstar}"

if [ -n "${DOGFOOD_OUT_DIR:-}" ]; then
  WORKDIR="$DOGFOOD_OUT_DIR"
  mkdir -p "$WORKDIR"
else
  WORKDIR="$(mktemp -d)"
fi

cleanup() {
  local status="$?"
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$KEEP" = "1" ] || [ -n "${DOGFOOD_OUT_DIR:-}" ]; then
    echo "dogfood artifacts kept at $WORKDIR" >&2
  else
    rm -rf "$WORKDIR"
  fi
  exit "$status"
}
trap cleanup EXIT

cd "$ROOT"

if [ ! -x "$MOLSTAR_BIN" ]; then
  LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
  mkdir -p "$(dirname "$MOLSTAR_BIN")"
  go build -ldflags "$LDFLAGS" -o "$MOLSTAR_BIN" ./cmd/molstar
fi

json_ok() {
  node -e 'const fs=require("fs"); const v=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if (v.ok !== true) { console.error(JSON.stringify(v,null,2)); process.exit(1); }' "$1"
}

json_rpc_ok() {
  node -e 'const fs=require("fs"); const v=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if (v.jsonrpc !== "2.0" || !v.result || v.result.ok !== true) { console.error(JSON.stringify(v,null,2)); process.exit(1); }' "$1"
}

OUT="$WORKDIR/out"
RUNS="$WORKDIR/runs"
BIN_DIR="$WORKDIR/bin"
mkdir -p "$OUT" "$RUNS" "$BIN_DIR"
export MOLSTAR_RUNS_DIR="$RUNS"
export MOLSTAR_RENDER=""
export MOLSTAR_RENDER_FALLBACK=""
export MOLSTAR_VALIDATE=""

"$MOLSTAR_BIN" version --json >"$WORKDIR/version.json"
json_ok "$WORKDIR/version.json"
"$MOLSTAR_BIN" doctor --skip-probe --json >"$WORKDIR/doctor.json"
json_ok "$WORKDIR/doctor.json"
"$MOLSTAR_BIN" capabilities --json --timeout 15s >"$WORKDIR/capabilities.json"
json_ok "$WORKDIR/capabilities.json"
"$MOLSTAR_BIN" selectors explain 'chain:A/residue:1-2' --json >"$WORKDIR/selector.json"
json_ok "$WORKDIR/selector.json"
"$MOLSTAR_BIN" presets list --json >"$WORKDIR/presets.json"
json_ok "$WORKDIR/presets.json"

"$MOLSTAR_BIN" install-local \
  --home "$ROOT" \
  --bin-dir "$BIN_DIR" \
  --config "$WORKDIR/config.json" \
  --name molstar-dogfood \
  --force \
  --json >"$WORKDIR/install-local.json"
json_ok "$WORKDIR/install-local.json"
"$BIN_DIR/molstar-dogfood" doctor --skip-probe --json >"$WORKDIR/installed-doctor.json"
json_ok "$WORKDIR/installed-doctor.json"

MODEL="$WORKDIR/one.cif"
cp "$ROOT/internal/cli/testdata/compat/one.cif" "$MODEL"
"$MOLSTAR_BIN" recipe init ligand --path "$MODEL" --image-out "$OUT/recipe.png" --size 128x96 --out "$WORKDIR/local.recipe.yaml"
"$MOLSTAR_BIN" recipe validate "$WORKDIR/local.recipe.yaml" --schema --json >"$WORKDIR/recipe-validate.json"
json_ok "$WORKDIR/recipe-validate.json"
"$MOLSTAR_BIN" recipe compile "$WORKDIR/local.recipe.yaml" --out "$WORKDIR/local.job.yaml" --json >"$WORKDIR/recipe-compile.json"
json_ok "$WORKDIR/recipe-compile.json"
"$MOLSTAR_BIN" job explain "$WORKDIR/local.job.yaml" --offline --json >"$WORKDIR/job-explain.json"
json_ok "$WORKDIR/job-explain.json"
"$MOLSTAR_BIN" inspect "$WORKDIR/local.job.yaml" --offline --semantic=false --json >"$WORKDIR/inspect.json"
json_ok "$WORKDIR/inspect.json"
"$MOLSTAR_BIN" scene compile "$WORKDIR/local.job.yaml" --offline --out "$WORKDIR/local.mvsj" --json >"$WORKDIR/scene-compile.json"
json_ok "$WORKDIR/scene-compile.json"
"$MOLSTAR_BIN" scene validate "$WORKDIR/local.mvsj" --json >"$WORKDIR/scene-validate.json"
json_ok "$WORKDIR/scene-validate.json"
"$MOLSTAR_BIN" render "$WORKDIR/local.job.yaml" --offline --run-label local-recipe --json >"$WORKDIR/render-local.json"
json_ok "$WORKDIR/render-local.json"
test -s "$OUT/recipe.png"

"$MOLSTAR_BIN" quickstart --out "$WORKDIR/quickstart" --json >"$WORKDIR/quickstart.json"
json_ok "$WORKDIR/quickstart.json"
test -s "$WORKDIR/quickstart/demo.png"

"$MOLSTAR_BIN" render --demo --out "$OUT/compact.png" --size 96x72 --json --compact >"$WORKDIR/render-compact.json"
json_ok "$WORKDIR/render-compact.json"
node -e 'const fs=require("fs"); const v=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if ("job" in v || "mvs_document" in v || (v.commands || []).some(c => "stdout" in c || "stderr" in c)) { console.error(JSON.stringify(v,null,2)); process.exit(1); }' "$WORKDIR/render-compact.json"
test -s "$OUT/compact.png"

"$MOLSTAR_BIN" render --demo --out "$OUT/demo.png" --mvs "$OUT/demo.mvsj" --state "$OUT/demo.molj" --size 128x96 --run-label dogfood-demo --json >"$WORKDIR/render-demo.json"
json_ok "$WORKDIR/render-demo.json"
RUN_ID="$("$MOLSTAR_BIN" logs --last --json | node -e 'let s=""; process.stdin.on("data", d => s += d); process.stdin.on("end", () => console.log(JSON.parse(s).run.id))')"
"$MOLSTAR_BIN" diagnose "$RUN_ID" --json >"$WORKDIR/diagnose-run.json"
json_ok "$WORKDIR/diagnose-run.json"
"$MOLSTAR_BIN" logs show "$RUN_ID" --rerun --out-dir "$OUT/rerun" --json >"$WORKDIR/rerun.json"
json_ok "$WORKDIR/rerun.json"
"$MOLSTAR_BIN" logs export "$RUN_ID" --out "$WORKDIR/run.molrun" --json >"$WORKDIR/export.json"
json_ok "$WORKDIR/export.json"
"$MOLSTAR_BIN" logs verify "$WORKDIR/run.molrun" --strict --json >"$WORKDIR/verify-run.json"
json_ok "$WORKDIR/verify-run.json"
"$MOLSTAR_BIN" logs rerun "$WORKDIR/run.molrun" --out-dir "$OUT/bundle-rerun" --json >"$WORKDIR/bundle-rerun.json"
json_ok "$WORKDIR/bundle-rerun.json"
test -s "$OUT/bundle-rerun/demo.png"
"$MOLSTAR_BIN" logs import "$WORKDIR/run.molrun" --dir "$WORKDIR/imported-runs" --json >"$WORKDIR/import.json"
json_ok "$WORKDIR/import.json"
"$MOLSTAR_BIN" diagnose "$RUN_ID" --bundle --out "$WORKDIR/issue.zip" --json >"$WORKDIR/issue.json"
json_ok "$WORKDIR/issue.json"
test -s "$WORKDIR/issue.zip"
"$MOLSTAR_BIN" diagnose "$RUN_ID" --bundle --out "$WORKDIR/issue-redacted.zip" --redact-paths --redact-env --redact-inputs --json >"$WORKDIR/issue-redacted.json"
json_ok "$WORKDIR/issue-redacted.json"
test -s "$WORKDIR/issue-redacted.zip"

"$MOLSTAR_BIN" logs export "$RUN_ID" --out "$WORKDIR/no-inputs.molrun" --include-inputs=false --json >"$WORKDIR/export-no-inputs.json"
json_ok "$WORKDIR/export-no-inputs.json"
"$MOLSTAR_BIN" logs verify "$WORKDIR/no-inputs.molrun" --json >"$WORKDIR/verify-no-inputs.json"
json_ok "$WORKDIR/verify-no-inputs.json"
node -e 'const fs=require("fs"); const v=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if (v.replayable) process.exit(1)' "$WORKDIR/verify-no-inputs.json"

set +e
"$MOLSTAR_BIN" render --demo --focus missing-component --ci-artifact "$WORKDIR/ci-failure" --json >"$WORKDIR/render-failure.json" 2>"$WORKDIR/render-failure.err"
FAIL_STATUS="$?"
set -e
if [ "$FAIL_STATUS" -eq 0 ]; then
  echo "expected invalid focus render to fail" >&2
  exit 1
fi
"$MOLSTAR_BIN" diagnose --ci-artifact "$WORKDIR/ci-failure" --json >"$WORKDIR/diagnose-ci.json"
json_ok "$WORKDIR/diagnose-ci.json"
FAILED_RUN_ID="$("$MOLSTAR_BIN" logs list --failed --json | node -e 'let s=""; process.stdin.on("data", d => s += d); process.stdin.on("end", () => console.log(JSON.parse(s).runs[0].id))')"
MOLSTAR_BIN="$MOLSTAR_BIN" ./scripts/collect-molstar-ci-artifact.sh \
  --run-id "$FAILED_RUN_ID" \
  --runs-dir "$RUNS" \
  --ci-artifact "$WORKDIR/ci-failure" \
  --out "$WORKDIR/ci-issue.zip" >"$WORKDIR/collect-ci-issue.out"
test -s "$WORKDIR/ci-issue.zip"
json_ok "$WORKDIR/ci-issue.zip.json"

"$MOLSTAR_BIN" job normalize "$MODEL" --offline --out "$OUT/batch-one.png" --size 96x72 --write "$WORKDIR/batch-one.json" --format json --json >/dev/null
"$MOLSTAR_BIN" job normalize "$MODEL" --offline --out "$OUT/batch-two.png" --size 96x72 --write "$WORKDIR/batch-two.json" --format json --json >/dev/null
node -e 'const fs=require("fs"); process.stdout.write(process.argv.slice(1).map(p => JSON.stringify(JSON.parse(fs.readFileSync(p, "utf8")))).join("\n") + "\n")' "$WORKDIR/batch-one.json" "$WORKDIR/batch-two.json" >"$WORKDIR/jobs.jsonl"
"$MOLSTAR_BIN" batch "$WORKDIR/jobs.jsonl" --offline --concurrency 2 --json --manifest "$WORKDIR/batch-manifest.json" >"$WORKDIR/batch.out"
node -e 'const fs=require("fs"); const lines=fs.readFileSync(process.argv[1],"utf8").trim().split(/\n+/).map(JSON.parse); if (!lines.every(x => x.ok)) { console.error(JSON.stringify(lines,null,2)); process.exit(1); }' "$WORKDIR/batch.out"
test -s "$WORKDIR/batch-manifest.json"

"$MOLSTAR_BIN" compat check --out-dir "$WORKDIR/compat" --json >"$WORKDIR/compat.json"
json_ok "$WORKDIR/compat.json"
"$MOLSTAR_BIN" fixtures verify --out-dir "$WORKDIR/fixtures" --json >"$WORKDIR/fixtures.json"
json_ok "$WORKDIR/fixtures.json"
"$MOLSTAR_BIN" bench "$MODEL" --offline --dry-run --iterations 1 --warmup 0 --out-dir "$WORKDIR/bench" --json >"$WORKDIR/bench.json" 2>"$WORKDIR/bench.stderr"
json_ok "$WORKDIR/bench.json"

SOCKET="$WORKDIR/molstar.sock"
"$MOLSTAR_BIN" serve --socket "$SOCKET" --job-store "$WORKDIR/server-jobs" --no-worker --quiet >"$WORKDIR/server.out" 2>"$WORKDIR/server.err" &
SERVER_PID="$!"
for _ in $(seq 1 100); do
  [ -S "$SOCKET" ] && break
  sleep 0.1
done
if [ ! -S "$SOCKET" ]; then
  cat "$WORKDIR/server.err" >&2
  exit 1
fi
"$MOLSTAR_BIN" serve smoke --socket "$SOCKET" --json >"$WORKDIR/serve-smoke.json"
json_ok "$WORKDIR/serve-smoke.json"
"$MOLSTAR_BIN" serve smoke --socket "$SOCKET" --render-probe --probe-out-dir "$WORKDIR/serve-smoke-probe" --json >"$WORKDIR/serve-smoke-probe.json"
json_ok "$WORKDIR/serve-smoke-probe.json"
test -s "$WORKDIR/serve-smoke-probe/downloads/serve-smoke.png"
"$MOLSTAR_BIN" rpc capabilities --socket "$SOCKET" --json >"$WORKDIR/rpc-capabilities.json"
json_rpc_ok "$WORKDIR/rpc-capabilities.json"
"$MOLSTAR_BIN" server submit "$WORKDIR/local.job.yaml" --socket "$SOCKET" --wait --download-outputs --out-dir "$OUT/server" --json >"$WORKDIR/server-submit.json"
json_ok "$WORKDIR/server-submit.json"
kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
unset SERVER_PID

python3 - "$WORKDIR" >"$WORKDIR/summary.json" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
checks = [
    "doctor",
    "install-local",
    "recipe",
    "quickstart",
    "render",
    "compact-render",
    "logs-verify",
    "logs-rerun",
    "diagnose-bundle",
    "diagnose-redacted-bundle",
    "ci-diagnose",
    "ci-issue-collector",
    "batch",
    "compat",
    "fixtures",
    "bench",
    "serve-smoke",
    "serve-smoke-render-probe",
    "server-submit",
]
print(json.dumps({"ok": True, "root": str(root), "checks": checks}, indent=2))
PY

cat "$WORKDIR/summary.json"
