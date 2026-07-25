#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_BIN="${DOCKER_BIN:-docker}"
OUT_DIR="${VERIFY_LINUX_CLEAN_OUT_DIR:-$ROOT/dist/linux-clean}"
LOG_DIR="$OUT_DIR/logs"
REPORT_PATH="${VERIFY_LINUX_CLEAN_REPORT:-$OUT_DIR/verify-linux-clean-report.json}"
STEP_LOG="${VERIFY_LINUX_CLEAN_STEP_LOG:-$OUT_DIR/verify-linux-clean-steps.jsonl}"
NO_CACHE="${VERIFY_LINUX_CLEAN_NO_CACHE:-1}"
RUN_RELEASE="${VERIFY_LINUX_CLEAN_RELEASE:-1}"
RUN_RUNTIME="${VERIFY_LINUX_CLEAN_RUNTIME:-1}"
RUN_ARTIFACT="${VERIFY_LINUX_CLEAN_ARTIFACT:-1}"
RELEASE_RUN_RETRIES="${VERIFY_LINUX_CLEAN_RELEASE_RETRIES:-2}"
RELEASE_GO_CACHE="${VERIFY_LINUX_CLEAN_RELEASE_GO_CACHE:-1}"
RELEASE_GO_PARALLELISM="${VERIFY_LINUX_CLEAN_RELEASE_GO_PARALLELISM:-2}"
DEV_TEST_IMAGE="${VERIFY_LINUX_CLEAN_DEV_IMAGE:-molstar:clean-dev-test}"
RUNTIME_IMAGE="${VERIFY_LINUX_CLEAN_RUNTIME_IMAGE:-molstar:clean-runtime}"

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
mkdir -p "$OUT_DIR" "$LOG_DIR"
: >"$STEP_LOG"
cd "$ROOT"

record_step() {
  local name="$1"
  local status="$2"
  local code="$3"
  local started_at="$4"
  local duration_s="$5"
  local log_path="$6"
  python3 - "$STEP_LOG" "$name" "$status" "$code" "$started_at" "$duration_s" "$log_path" <<'PY'
import json
import sys

path, name, status, code, started_at, duration_s, log_path = sys.argv[1:]
with open(path, "a") as f:
    f.write(json.dumps({
        "name": name,
        "status": status,
        "code": int(code),
        "started_at": int(started_at),
        "duration_s": int(duration_s),
        "log": log_path,
    }, sort_keys=True) + "\n")
PY
}

write_report() {
  local status="$1"
  local failed_step="${2:-}"
  local failed_code="${3:-0}"
  python3 - "$STEP_LOG" "$REPORT_PATH" "$status" "$failed_step" "$failed_code" "$OUT_DIR" "$DEV_TEST_IMAGE" "$RUNTIME_IMAGE" <<'PY'
import glob
import json
import pathlib
import sys
import time

steps_path, report_path, status, failed_step, failed_code, out_dir, dev_image, runtime_image = sys.argv[1:]
steps = []
if pathlib.Path(steps_path).exists():
    for line in pathlib.Path(steps_path).read_text().splitlines():
        if line.strip():
            steps.append(json.loads(line))
artifacts = sorted(glob.glob(str(pathlib.Path(out_dir) / "release" / "package-dist" / "headlessmolstar-local-*.tar.gz")))
report = {
    "ok": status == "passed",
    "status": status,
    "failed_step": failed_step or None,
    "failed_code": int(failed_code),
    "out_dir": out_dir,
    "dev_test_image": dev_image,
    "runtime_image": runtime_image,
    "linux_artifacts": artifacts,
    "steps": steps,
    "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
pathlib.Path(report_path).write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
PY
}

safe_step_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '-'
}

dump_processes() {
  echo "active clean-verifier processes:" >&2
  ps -o pid,ppid,stat,etime,command -ax | grep -E 'verify-linux-clean|verify-release|verify-docker|docker build|docker run|go test|go vet|package-local' | grep -v grep >&2 || true
}

run_step() {
  local name="$1"
  shift
  local started_at
  started_at="$(date +%s)"
  local log_path="$LOG_DIR/$(safe_step_name "$name").log"
  printf '==> clean: %s\n' "$name" >&2
  set +e
  "$@" >"$log_path" 2>&1
  local code="$?"
  set -e
  local ended_at
  ended_at="$(date +%s)"
  local duration_s=$((ended_at - started_at))
  if [ "$code" -eq 0 ]; then
    record_step "$name" "passed" "$code" "$started_at" "$duration_s" "$log_path"
    write_report "running"
    printf '<== clean: %s passed in %ss\n' "$name" "$duration_s" >&2
    return 0
  fi
  record_step "$name" "failed" "$code" "$started_at" "$duration_s" "$log_path"
  write_report "failed" "$name" "$code"
  echo "clean verifier step failed: $name (exit $code, ${duration_s}s)" >&2
  echo "log: $log_path" >&2
  tail -n 120 "$log_path" >&2 || true
  dump_processes
  exit "$code"
}

record_skip() {
  local name="$1"
  record_step "$name" "skipped" 0 "$(date +%s)" 0 ""
  write_report "running"
}

docker_build() {
  local image="$1"
  shift
  local args=(-t "$image")
  if [ "$NO_CACHE" = "1" ]; then
    args+=(--no-cache --pull)
  fi
  "$DOCKER_BIN" build "${args[@]}" "$@" "$ROOT"
}

latest_linux_artifact() {
  python3 - "$OUT_DIR/release/package-dist" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
matches = sorted(root.glob("headlessmolstar-local-linux-*.tar.gz"), key=lambda p: p.stat().st_mtime)
print(matches[-1] if matches else "")
PY
}

on_signal() {
  local signal="$1"
  echo "verify-linux-clean interrupted by $signal" >&2
  dump_processes
  write_report "interrupted" "$signal" 143
  exit 143
}
trap 'on_signal TERM' TERM
trap 'on_signal INT' INT

needs_docker=0
for enabled in "$RUN_RELEASE" "$RUN_RUNTIME" "$RUN_ARTIFACT"; do
  if [ "$enabled" = "1" ]; then
    needs_docker=1
  fi
done
if [ "$needs_docker" = "1" ]; then
  run_step "docker available" "$DOCKER_BIN" version
else
  record_skip "docker available"
fi
run_step "script syntax" bash -c '
  for script in "$@"; do
    bash -n "$script"
  done
' bash \
  "$ROOT/scripts/package-local.sh" \
  "$ROOT/scripts/verify-release-docker.sh" \
  "$ROOT/scripts/verify-docker.sh" \
  "$ROOT/scripts/verify-linux-clean.sh"
run_step "review grouping" bash -c 'REVIEW_GROUPS_FORMAT=json "$1/scripts/review-change-groups.sh" >"$2/review-change-groups.json"' bash "$ROOT" "$OUT_DIR"

if [ "$RUN_RELEASE" = "1" ]; then
  run_step "linux release verifier" env \
    DOCKER_BIN="$DOCKER_BIN" \
    DEV_TEST_IMAGE="$DEV_TEST_IMAGE" \
    RUNTIME_IMAGE="$RUNTIME_IMAGE" \
    VERIFY_RELEASE_DOCKER_NO_CACHE="$NO_CACHE" \
    VERIFY_RELEASE_DOCKER_RUN_RETRIES="$RELEASE_RUN_RETRIES" \
    VERIFY_RELEASE_DOCKER_GO_CACHE="$RELEASE_GO_CACHE" \
    VERIFY_RELEASE_DOCKER_GO_PARALLELISM="$RELEASE_GO_PARALLELISM" \
    VERIFY_RELEASE_DOCKER_OUT_DIR="$OUT_DIR/release" \
    "$ROOT/scripts/verify-release-docker.sh" --skip-runtime-docker
else
  record_skip "linux release verifier"
fi

if [ "$RUN_RUNTIME" = "1" ]; then
  run_step "runtime docker build" docker_build "$RUNTIME_IMAGE"
  run_step "runtime docker verify" env \
    DOCKER_BIN="$DOCKER_BIN" \
    IMAGE="$RUNTIME_IMAGE" \
    DOCKER_VERIFY_PROFILE=runtime \
    DOCKER_VERIFY_OUT_DIR="$OUT_DIR/runtime" \
    "$ROOT/scripts/verify-docker.sh"
else
  record_skip "runtime docker build"
  record_skip "runtime docker verify"
fi

if [ "$RUN_ARTIFACT" = "1" ]; then
  artifact="$(latest_linux_artifact)"
  if [ -n "$artifact" ]; then
    run_step "linux artifact docker verify" env \
      DOCKER_BIN="$DOCKER_BIN" \
      IMAGE="$RUNTIME_IMAGE" \
      ARTIFACT="$artifact" \
      DOCKER_VERIFY_OUT_DIR="$OUT_DIR/linux-artifact" \
      "$ROOT/scripts/verify-docker-artifact.sh"
  else
    record_step "linux artifact docker verify" "skipped" 0 "$(date +%s)" 0 ""
    write_report "running"
    echo "skipping Linux artifact Docker verify; no Linux package artifact found under $OUT_DIR/release/package-dist" >&2
  fi
else
  record_skip "linux artifact docker verify"
fi

write_report "passed"
printf 'clean Linux verification passed; report: %s\n' "$REPORT_PATH"
