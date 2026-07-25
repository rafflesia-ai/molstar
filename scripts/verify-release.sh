#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERIFY_DOCKER="${VERIFY_DOCKER:-0}"
VERIFY_DOCKER_RENDER="${VERIFY_DOCKER_RENDER:-0}"
for arg in "$@"; do
  case "$arg" in
    --docker) VERIFY_DOCKER=1 ;;
    --render-docker) VERIFY_DOCKER=1; VERIFY_DOCKER_RENDER=1 ;;
    --skip-docker) VERIFY_DOCKER=0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

cd "$ROOT"

DIST_DIR="${VERIFY_RELEASE_DIST_DIR:-$ROOT/dist}"
ensure_root_dist_target() {
  if [ -L "$ROOT/dist" ] && [ ! -e "$ROOT/dist" ]; then
    target="$(readlink "$ROOT/dist")"
    case "$target" in
      /*) mkdir -p "$target" ;;
      *) mkdir -p "$ROOT/$target" ;;
    esac
  fi
}
ensure_root_dist_target
LOCK_DIR="$DIST_DIR/.verify-release.lock"
LOCK_WAIT_SECONDS="${VERIFY_RELEASE_LOCK_WAIT_SECONDS:-300}"
mkdir -p "$DIST_DIR"
LOCK_STARTED_AT="$(date +%s)"
while ! mkdir "$LOCK_DIR" 2>/dev/null; do
  old_pid=""
  if [ -f "$LOCK_DIR/pid" ]; then
    old_pid="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
  fi
  if [ -n "$old_pid" ] && ! kill -0 "$old_pid" 2>/dev/null; then
    echo "breaking stale verify-release lock from pid $old_pid" >&2
    rm -rf "$LOCK_DIR"
    continue
  fi
  now="$(date +%s)"
  if [ $((now - LOCK_STARTED_AT)) -ge "$LOCK_WAIT_SECONDS" ]; then
    echo "verify-release is already running${old_pid:+ as pid $old_pid}; remove $LOCK_DIR only if no release verifier is active" >&2
    if [ -n "$old_pid" ]; then
      ps -o pid,ppid,stat,etime,command -p "$old_pid" >&2 || true
    fi
    exit 7
  fi
  sleep 1
done
printf '%s\n' "$$" >"$LOCK_DIR/pid"

tmp_schema="$(mktemp)"
tmp_artifact=""
tmp_selftest=""
REPORT_PATH="${VERIFY_RELEASE_REPORT:-$DIST_DIR/verify-release-report.json}"
STEP_LOG="${VERIFY_RELEASE_STEP_LOG:-$DIST_DIR/verify-release-steps.jsonl}"
VERIFY_RELEASE_OUT_DIR="${VERIFY_RELEASE_OUT_DIR:-$DIST_DIR/verify-release}"
ARTIFACT_PATH_FILE="$DIST_DIR/.verify-release-artifact-path"
cleanup() {
  rm -f "$tmp_schema"
  if [ -n "$tmp_artifact" ]; then
    rm -f "$tmp_artifact"
  fi
  if [ -n "$tmp_selftest" ]; then
    rm -rf "$tmp_selftest"
  fi
  rm -rf "$LOCK_DIR"
}
trap cleanup EXIT

VERIFY_RELEASE_GO_PARALLELISM="${VERIFY_RELEASE_GO_PARALLELISM:-4}"
export GOMAXPROCS="${GOMAXPROCS:-$VERIFY_RELEASE_GO_PARALLELISM}"
if [[ " ${GOFLAGS:-} " != *" -p="* ]]; then
  export GOFLAGS="${GOFLAGS:-} -p=$VERIFY_RELEASE_GO_PARALLELISM"
fi
mkdir -p "$(dirname "$REPORT_PATH")" "$(dirname "$STEP_LOG")" "$VERIFY_RELEASE_OUT_DIR"
: >"$STEP_LOG"

write_release_report() {
  local status="$1"
  local failed_step="${2:-}"
  local failed_code="${3:-0}"
  python3 - "$STEP_LOG" "$REPORT_PATH" "$status" "$failed_step" "$failed_code" <<'PY'
import json
import pathlib
import sys
import time

steps_path = pathlib.Path(sys.argv[1])
report_path = pathlib.Path(sys.argv[2])
status = sys.argv[3]
failed_step = sys.argv[4]
failed_code = int(sys.argv[5])
steps = []
if steps_path.exists():
    for line in steps_path.read_text().splitlines():
        if line.strip():
            steps.append(json.loads(line))
report = {
    "ok": status == "passed",
    "status": status,
    "failed_step": failed_step or None,
    "failed_code": failed_code,
    "steps": steps,
    "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
report_path.parent.mkdir(parents=True, exist_ok=True)
report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
PY
}

record_step() {
  local name="$1"
  local status="$2"
  local code="$3"
  local started_at="$4"
  local duration_s="$5"
  python3 - "$STEP_LOG" "$name" "$status" "$code" "$started_at" "$duration_s" <<'PY'
import json
import sys

path, name, status, code, started_at, duration_s = sys.argv[1:]
with open(path, "a") as f:
    f.write(json.dumps({
        "name": name,
        "status": status,
        "code": int(code),
        "started_at": int(started_at),
        "duration_s": int(duration_s),
    }, sort_keys=True) + "\n")
PY
}

dump_release_processes() {
  echo "active release-related processes:" >&2
  ps -o pid,ppid,stat,etime,command -ax | grep -E 'verify-release|go test|go vet|molstar|render-mvs|package-local|verify-artifact' | grep -v grep >&2 || true
}

on_signal() {
  local signal="$1"
  echo "verify-release interrupted by $signal" >&2
  dump_release_processes
  write_release_report "interrupted" "$signal" 143
  exit 143
}
trap 'on_signal TERM' TERM
trap 'on_signal INT' INT

run_step() {
  local name="$1"
  shift
  local started_at
  started_at="$(date +%s)"
  printf '==> %s\n' "$name" >&2
  set +e
  "$@"
  local code="$?"
  set -e
  local ended_at
  ended_at="$(date +%s)"
  local duration_s=$((ended_at - started_at))
  if [ "$code" -eq 0 ]; then
    record_step "$name" "passed" "$code" "$started_at" "$duration_s"
    write_release_report "running"
    printf '<== %s passed in %ss\n' "$name" "$duration_s" >&2
    return 0
  fi
  record_step "$name" "failed" "$code" "$started_at" "$duration_s"
  write_release_report "failed" "$name" "$code"
  echo "release step failed: $name (exit $code, ${duration_s}s)" >&2
  dump_release_processes
  exit "$code"
}

if [ "${VERIFY_RELEASE_SKIP_GO_TESTS:-0}" = "1" ]; then
  record_step "go test" "skipped" 0 "$(date +%s)" 0
else
  run_step "go test" go test ./...
fi
run_step "go vet" go vet ./...
run_step "typecheck" npm run typecheck
run_step "render script syntax" node --check scripts/render-mvs.js
run_step "dependency audit" bash -c 'AUDIT_OUT="$1/audit" npm run audit:deps >/dev/null' bash "$DIST_DIR"

run_step "generated assets" "$ROOT/scripts/verify-generated-assets.sh"

LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
run_step "build molstar" go build -ldflags "$LDFLAGS" -o bin/molstar ./cmd/molstar
run_step "generate cli assets" ./scripts/generate-cli-assets.sh
run_step "molstar doctor" bash -c 'bin/molstar doctor --skip-probe --json >"$1/molstar-doctor.json"' bash "$VERIFY_RELEASE_OUT_DIR"
run_step "molstar dogfood" bash -c 'MOLSTAR_BIN="$1/bin/molstar" ./scripts/dogfood-molstar.sh >/dev/null' bash "$ROOT"

run_step "goreleaser check" bash -c 'if command -v goreleaser >/dev/null 2>&1; then goreleaser check; else echo "warning: goreleaser not found; skipping goreleaser check" >&2; fi'

if [ -d "$ROOT/node_modules" ] && [ "${VERIFY_RELEASE_REBUILD_NODE_MODULES:-0}" != "1" ]; then
  run_step "package local" bash -c 'PACKAGE_DIST_DIR="$1" PACKAGE_USE_EXISTING_NODE_MODULES="${PACKAGE_USE_EXISTING_NODE_MODULES:-1}" ./scripts/package-local.sh >"$2"' bash "$DIST_DIR" "$ARTIFACT_PATH_FILE"
else
  run_step "package local" bash -c 'PACKAGE_DIST_DIR="$1" ./scripts/package-local.sh >"$2"' bash "$DIST_DIR" "$ARTIFACT_PATH_FILE"
fi
artifact="$(cat "$ARTIFACT_PATH_FILE")"
tmp_artifact="$(mktemp "${TMPDIR:-/tmp}/molstar-artifact.XXXXXX.tar.gz")"
cp "$artifact" "$tmp_artifact"
run_step "verify molstar artifact" bash -c 'ARTIFACT="$1" ./scripts/verify-artifact-molstar.sh' bash "$tmp_artifact"

if [ "$VERIFY_DOCKER" = "1" ]; then
  run_step "docker build" docker build -t molstar:release-check .
  run_step "docker molstar dogfood" bash -c 'DOGFOOD_DOCKER_BUILD=0 DOGFOOD_DOCKER_RENDER="$1" IMAGE=molstar:release-check ./scripts/dogfood-molstar-docker.sh' bash "$VERIFY_DOCKER_RENDER"
  case "$(basename "$artifact")" in
    *linux*)
      run_step "docker artifact verify" bash -c 'ARTIFACT="$1" IMAGE=molstar:release-check ./scripts/verify-docker-artifact.sh' bash "$tmp_artifact"
      ;;
    *)
      echo "skipping docker artifact verify for non-Linux artifact $(basename "$artifact")" >&2
      record_step "docker artifact verify" "skipped" 0 "$(date +%s)" 0
      ;;
  esac
else
  record_step "docker build" "skipped" 0 "$(date +%s)" 0
fi

write_release_report "passed"
printf 'release verification passed\n'
