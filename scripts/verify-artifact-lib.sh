#!/usr/bin/env bash

ARTIFACT_TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

resolve_artifact() {
  local requested="${ARTIFACT:-${1:-}}"
  if [ -z "$requested" ]; then
    local matches=()
    shopt -s nullglob
    matches=(
      "$ARTIFACT_TEST_ROOT"/dist/headlessmolstar-local-*.tar.gz
      "$ARTIFACT_TEST_ROOT"/dist/headlessmolstar-local-*.zip
    )
    shopt -u nullglob
    if [ "${#matches[@]}" -gt 0 ]; then
      requested="$(ls -t "${matches[@]}" | head -n 1)"
    fi
  fi
  if [ -z "$requested" ] || [ ! -f "$requested" ]; then
    echo "artifact not found; run npm run package:local or pass ARTIFACT=/path/to/archive.tar.gz|zip" >&2
    return 2
  fi
  printf '%s\n' "$requested"
}

list_artifact() {
  local artifact_path="$1"
  case "$artifact_path" in
    *.zip)
      python3 - "$artifact_path" <<'PY'
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as archive:
    for name in archive.namelist():
        print(name)
PY
      ;;
    *)
      tar -tzf "$artifact_path"
      ;;
  esac
}

verify_artifact_contents() {
  local artifact_path="$1"
  local list_path="$2"
  list_artifact "$artifact_path" >"$list_path"
  if grep -E '(^|/)(\._[^/]+|\.DS_Store)$' "$list_path" >/dev/null; then
    echo "artifact contains macOS metadata files:" >&2
    grep -E '(^|/)(\._[^/]+|\.DS_Store)$' "$list_path" >&2
    return 1
  fi
  require_archive_path_any "$list_path" "bin/molstar" "molstar" "bin/molstar.exe" "molstar.exe"
  for required in \
    "scripts/render-mvs.js" \
    "docs/cli/molstar.md" \
    "completions/molstar.bash" \
    "schema/headlessmolstar-job-v1.schema.json" \
    "python/headlessmolstar/__init__.py"; do
    if ! grep -E "(^|/)$required$" "$list_path" >/dev/null; then
      echo "artifact is missing $required" >&2
      return 1
    fi
  done
}

require_archive_path_any() {
  local list_path="$1"
  shift
  for candidate in "$@"; do
    if grep -E "(^|/)$candidate$" "$list_path" >/dev/null; then
      return 0
    fi
  done
  echo "artifact is missing one of: $*" >&2
  return 1
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

install_artifact_runtime() {
  local artifact_path="$1"
  local workdir="$2"

  mkdir -p "$workdir/bin" "$workdir/work"
  (cd "$ARTIFACT_TEST_ROOT" && go run ./cmd/molstar install-artifact \
    --artifact "$artifact_path" \
    --prefix "$workdir/runtime" \
    --bin-dir "$workdir/bin" \
    --config "$workdir/config.json" \
    --force \
    --json >"$workdir/install.json")

  export PATH="$workdir/bin:$PATH"
  export MOLSTAR_CONFIG="$workdir/config.json"
  export MOLSTAR_RENDER=
  export MOLSTAR_RENDER_FALLBACK=
  export MOLSTAR_VALIDATE=
}
