#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export COPYFILE_DISABLE=1
GOOS_VALUE="$(go env GOOS)"
GOARCH_VALUE="$(go env GOARCH)"
RUNTIME_NAME="headlessmolstar-local-${GOOS_VALUE}-${GOARCH_VALUE}"
DIST_DIR="${PACKAGE_DIST_DIR:-$ROOT/dist}"
LOCK_DIR="$DIST_DIR/.package.lock"
ARTIFACT="$DIST_DIR/${RUNTIME_NAME}.tar.gz"
WORK_PARENT="${PACKAGE_WORK_DIR:-${TMPDIR:-/tmp}}"
PACKAGE_LOG_DIR="${PACKAGE_LOG_DIR:-$DIST_DIR/package-logs}"
PACKAGE_REPORT="${PACKAGE_REPORT:-$PACKAGE_LOG_DIR/package-report.json}"
PACKAGE_STEP_LOG="${PACKAGE_STEP_LOG:-$PACKAGE_LOG_DIR/package-steps.jsonl}"
PACKAGE_STEP_TIMEOUT_SECONDS="${PACKAGE_STEP_TIMEOUT_SECONDS:-300}"
PACKAGE_BUILD_TIMEOUT_SECONDS="${PACKAGE_BUILD_TIMEOUT_SECONDS:-$PACKAGE_STEP_TIMEOUT_SECONDS}"
PACKAGE_DOCS_TIMEOUT_SECONDS="${PACKAGE_DOCS_TIMEOUT_SECONDS:-120}"
PACKAGE_COMPLETION_TIMEOUT_SECONDS="${PACKAGE_COMPLETION_TIMEOUT_SECONDS:-120}"
PACKAGE_NODE_MODULES_TIMEOUT_SECONDS="${PACKAGE_NODE_MODULES_TIMEOUT_SECONDS:-600}"
PACKAGE_TAR_TIMEOUT_SECONDS="${PACKAGE_TAR_TIMEOUT_SECONDS:-300}"

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
mkdir -p "$DIST_DIR" "$PACKAGE_LOG_DIR"
: >"$PACKAGE_STEP_LOG"
LOCK_WAIT_SECONDS="${PACKAGE_LOCK_WAIT_SECONDS:-300}"
LOCK_STARTED_AT="$(date +%s)"
while ! mkdir "$LOCK_DIR" 2>/dev/null; do
  if [ -f "$LOCK_DIR/pid" ]; then
    old_pid="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
    if [ -n "$old_pid" ] && ! kill -0 "$old_pid" 2>/dev/null; then
      rm -rf "$LOCK_DIR"
      continue
    fi
  elif [ "${PACKAGE_BREAK_STALE_LOCKS:-1}" = "1" ]; then
    rm -rf "$LOCK_DIR"
    continue
  fi
  now="$(date +%s)"
  if [ $((now - LOCK_STARTED_AT)) -ge "$LOCK_WAIT_SECONDS" ]; then
    echo "package-local is already running${old_pid:+ as pid $old_pid}; remove $LOCK_DIR only if no package process is active" >&2
    if [ -n "$old_pid" ]; then
      ps -o pid,ppid,stat,etime,command -p "$old_pid" >&2 || true
    fi
    exit 7
  fi
  sleep 1
done
printf '%s\n' "$$" >"$LOCK_DIR/pid"
cleanup_lock() {
  rm -rf "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup_lock EXIT

write_report() {
  local status="$1"
  local failed_step="${2:-}"
  local failed_code="${3:-0}"
  python3 - "$PACKAGE_STEP_LOG" "$PACKAGE_REPORT" "$status" "$failed_step" "$failed_code" "$ARTIFACT" <<'PY'
import json
import os
import pathlib
import sys
import time

steps_path, report_path, status, failed_step, failed_code, artifact = sys.argv[1:]
steps = []
path = pathlib.Path(steps_path)
if path.exists():
    for line in path.read_text().splitlines():
        if line.strip():
            steps.append(json.loads(line))
report = {
    "ok": status == "passed",
    "status": status,
    "failed_step": failed_step or None,
    "failed_code": int(failed_code),
    "artifact": artifact,
    "artifact_exists": os.path.exists(artifact),
    "steps": steps,
    "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
pathlib.Path(report_path).parent.mkdir(parents=True, exist_ok=True)
pathlib.Path(report_path).write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
PY
}

record_step() {
  local name="$1"
  local status="$2"
  local code="$3"
  local started_at="$4"
  local duration_s="$5"
  local log_path="$6"
  python3 - "$PACKAGE_STEP_LOG" "$name" "$status" "$code" "$started_at" "$duration_s" "$log_path" <<'PY'
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

dump_package_processes() {
  echo "active package-related processes:" >&2
  ps -o pid,ppid,stat,etime,command -ax | grep -E 'package-local|go build|molstar docs|completion|npm ci|npm rebuild|tar -C' | grep -v grep >&2 || true
}

run_with_timeout() {
  local seconds="$1"
  shift
  python3 - "$seconds" "$@" <<'PY'
import os
import signal
import subprocess
import sys

seconds = float(sys.argv[1])
command = sys.argv[2:]
if not command:
    print("run_with_timeout requires a command", file=sys.stderr)
    raise SystemExit(2)

process = subprocess.Popen(command, start_new_session=(os.name == "posix"))
try:
    raise SystemExit(process.wait(timeout=seconds))
except subprocess.TimeoutExpired:
    print(f"command timed out after {seconds:g}s: {' '.join(command)}", file=sys.stderr)
    if os.name == "posix":
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    else:
        process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        if os.name == "posix":
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        else:
            process.kill()
        process.wait()
    raise SystemExit(124)
PY
}

safe_step_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '-'
}

run_step() {
  local name="$1"
  local timeout="$2"
  shift 2
  local started_at
  started_at="$(date +%s)"
  local log_path="$PACKAGE_LOG_DIR/$(safe_step_name "$name").log"
  printf '==> package: %s\n' "$name" >&2
  set +e
  run_with_timeout "$timeout" "$@" >"$log_path" 2>&1
  local code="$?"
  set -e
  local ended_at
  ended_at="$(date +%s)"
  local duration_s=$((ended_at - started_at))
  if [ "$code" -eq 0 ]; then
    record_step "$name" "passed" "$code" "$started_at" "$duration_s" "$log_path"
    write_report "running"
    printf '<== package: %s passed in %ss\n' "$name" "$duration_s" >&2
    return 0
  fi
  record_step "$name" "failed" "$code" "$started_at" "$duration_s" "$log_path"
  write_report "failed" "$name" "$code"
  echo "package step failed: $name (exit $code, ${duration_s}s)" >&2
  echo "log: $log_path" >&2
  tail -n 80 "$log_path" >&2 || true
  dump_package_processes
  return "$code"
}

on_signal() {
  local signal="$1"
  echo "package-local interrupted by $signal" >&2
  dump_package_processes
  write_report "interrupted" "$signal" 143
  exit 143
}
trap 'on_signal TERM' TERM
trap 'on_signal INT' INT

remove_path() {
  local path="$1"
  if [ ! -e "$path" ]; then
    return 0
  fi
  chmod -R u+w "$path" 2>/dev/null || true
  for _ in 1 2 3; do
    rm -rf "$path" && return 0
    sleep 1
  done
  rm -rf "$path"
}

rebuild_node_modules() {
  npm ci --ignore-scripts
  for attempt in 1 2 3; do
    if npm rebuild; then
      return 0
    fi
    if [ "$attempt" = "3" ]; then
      return 1
    fi
    sleep 1
  done
}

copy_existing_node_modules() {
  if [ ! -d "$ROOT/node_modules" ]; then
    echo "PACKAGE_USE_EXISTING_NODE_MODULES=1 but $ROOT/node_modules is missing" >&2
    return 1
  fi
  cp -R -H "$ROOT/node_modules" "$RUNTIME_DIR/node_modules"
}

normalize_node_binary() {
  local node_path="$RUNTIME_DIR/node_modules/node/bin/node"
  local tmp_node="$RUNTIME_DIR/node_modules/.headlessmolstar-node"
  if [ ! -x "$node_path" ]; then
    echo "missing bundled Node binary at $node_path" >&2
    return 1
  fi
  cp "$node_path" "$tmp_node"
  rm -rf "$RUNTIME_DIR/node_modules/node/bin"
  mkdir -p "$RUNTIME_DIR/node_modules/node/bin"
  mv "$tmp_node" "$node_path"
  chmod 755 "$node_path"
}

remove_path "$DIST_DIR/package"
remove_path "$ARTIFACT"
for stale in "$DIST_DIR"/.package.*; do
  if [ -e "$stale" ] && [ "$stale" != "$LOCK_DIR" ]; then
    remove_path "$stale"
  fi
done
PACKAGE_DIR="$(mktemp -d "$WORK_PARENT/headlessmolstar-package.XXXXXX")"
RUNTIME_DIR="$PACKAGE_DIR/$RUNTIME_NAME"
TMP_ARTIFACT="$PACKAGE_DIR/${RUNTIME_NAME}.tar.gz"
cleanup_package() {
  remove_path "$PACKAGE_DIR" || true
}
cleanup_all() {
  code="$?"
  set +e
  cleanup_package
  cleanup_lock
  exit "$code"
}
trap cleanup_all EXIT
mkdir -p "$RUNTIME_DIR/bin"

LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
run_step "build molstar" "$PACKAGE_BUILD_TIMEOUT_SECONDS" bash -c 'cd "$1" && go build -ldflags "$2" -o "$3/bin/molstar" ./cmd/molstar' bash "$ROOT" "$LDFLAGS" "$RUNTIME_DIR"

for path in scripts python completions schema examples docs package.json package-lock.json .npmrc; do
  if [ -e "$ROOT/$path" ]; then
    cp -R "$ROOT/$path" "$RUNTIME_DIR/$path"
  fi
done

run_step "molstar docs" "$PACKAGE_DOCS_TIMEOUT_SECONDS" "$RUNTIME_DIR/bin/molstar" docs --out "$RUNTIME_DIR/docs/cli"
run_step "molstar completions" "$PACKAGE_COMPLETION_TIMEOUT_SECONDS" "$RUNTIME_DIR/bin/molstar" completion all --out-dir "$RUNTIME_DIR/completions"

if [ "${PACKAGE_USE_EXISTING_NODE_MODULES:-0}" = "1" ]; then
  run_step "copy node_modules" "$PACKAGE_NODE_MODULES_TIMEOUT_SECONDS" bash -c '
    root="$1"
    runtime="$2"
    if [ ! -d "$root/node_modules" ]; then
      echo "PACKAGE_USE_EXISTING_NODE_MODULES=1 but $root/node_modules is missing" >&2
      exit 1
    fi
    cp -R -H "$root/node_modules" "$runtime/node_modules"
  ' bash "$ROOT" "$RUNTIME_DIR"
else
  run_step "install node_modules" "$PACKAGE_NODE_MODULES_TIMEOUT_SECONDS" bash -c 'cd "$1" && npm ci --ignore-scripts && for attempt in 1 2 3; do npm rebuild && exit 0; sleep 1; done; exit 1' bash "$RUNTIME_DIR"
fi

run_step "normalize node binary" "$PACKAGE_STEP_TIMEOUT_SECONDS" bash -c '
  runtime="$1"
  node_path="$runtime/node_modules/node/bin/node"
  tmp_node="$runtime/node_modules/.headlessmolstar-node"
  if [ ! -x "$node_path" ]; then
    echo "missing bundled Node binary at $node_path" >&2
    exit 1
  fi
  cp "$node_path" "$tmp_node"
  rm -rf "$runtime/node_modules/node/bin"
  mkdir -p "$runtime/node_modules/node/bin"
  mv "$tmp_node" "$node_path"
  chmod 755 "$node_path"
' bash "$RUNTIME_DIR"
remove_path "$RUNTIME_DIR/node_modules/gl/angle/src/tests"
remove_path "$RUNTIME_DIR/node_modules/gl/angle/src/libANGLE/renderer/d3d/d3d11/winrt"
for path in \
  "$RUNTIME_DIR"/node_modules/node/node_modules/.bin \
  "$RUNTIME_DIR"/node_modules/node/node_modules/node-bin-* \
  "$RUNTIME_DIR"/node_modules/node/node_modules/node-bin-setup; do
  if [ -e "$path" ]; then
    remove_path "$path"
  fi
done

run_step "verify runtime symlinks" "$PACKAGE_STEP_TIMEOUT_SECONDS" bash -c 'python3 - "$1" <<'"'"'PY'"'"'
import os
import pathlib
import sys

root = pathlib.Path(sys.argv[1]).resolve()
errors = []
for current, dirs, files in os.walk(root):
    for name in dirs + files:
        path = pathlib.Path(current) / name
        if not path.is_symlink():
            continue
        target = os.readlink(path)
        if os.path.isabs(target):
            errors.append(f"{path.relative_to(root)} -> {target} is absolute")
            continue
        resolved = (path.parent / target).resolve()
        try:
            resolved.relative_to(root)
        except ValueError:
            errors.append(f"{path.relative_to(root)} -> {target} escapes runtime")
if errors:
    for error in errors:
        print(error, file=sys.stderr)
    raise SystemExit(1)
PY
' bash "$RUNTIME_DIR"

for attempt in 1 2 3; do
  remove_path "$TMP_ARTIFACT"
  if run_step "archive attempt $attempt" "$PACKAGE_TAR_TIMEOUT_SECONDS" bash -c 'tar -C "$1" -czf "$2" "$3" && gzip -t "$2" && tar -tzf "$2" >/dev/null' bash "$PACKAGE_DIR" "$TMP_ARTIFACT" "$RUNTIME_NAME"; then
    install -m 0644 "$TMP_ARTIFACT" "$ARTIFACT.tmp.$$"
    mv "$ARTIFACT.tmp.$$" "$ARTIFACT"
    write_report "passed"
    break
  fi
  if [ "$attempt" = "3" ]; then
    exit 1
  fi
  sleep 1
done
printf '%s\n' "$ARTIFACT"
