#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/verify-artifact-lib.sh"

ARTIFACT_PATH="$(resolve_artifact "${1:-}")"
WORKDIR="$(mktemp -d)"
SERVE_PID=""
cleanup() {
  local status="$?"
  if [ -n "$SERVE_PID" ] && kill -0 "$SERVE_PID" >/dev/null 2>&1; then
    kill "$SERVE_PID" >/dev/null 2>&1 || true
    wait "$SERVE_PID" >/dev/null 2>&1 || true
  fi
  if [ "${VERIFY_ARTIFACT_KEEP:-0}" = "1" ] || [ "$status" -ne 0 ]; then
    echo "molstar artifact verifier artifacts kept at $WORKDIR" >&2
  else
    rm -rf "$WORKDIR"
  fi
  exit "$status"
}
trap cleanup EXIT

install_artifact_runtime "$ARTIFACT_PATH" "$WORKDIR"

(
  cd "$WORKDIR/work"
  run_with_timeout 240 molstar self-test --json --timeout 180s >"$WORKDIR/self-test.json"
  run_with_timeout 180 molstar smoke --json --out-dir "$WORKDIR/smoke" >"$WORKDIR/smoke.json"
  run_with_timeout 180 molstar quickstart --json --out-dir "$WORKDIR/quickstart" >"$WORKDIR/quickstart.json"
)

python3 - "$WORKDIR/install.json" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    install = json.load(f)
sys.path.insert(0, install["home"] + "/python")
from headlessmolstar import HeadlessMolstar

report = HeadlessMolstar(["molstar"]).version(runtime=False)
assert report["ok"], report
PY

python3 - "$WORKDIR/work" <<'PY'
import json
import os
import sys

root = sys.argv[1]
cif = """data_one
_entry.id one
_cell.entry_id one
_cell.length_a 1
_cell.length_b 1
_cell.length_c 1
_cell.angle_alpha 90
_cell.angle_beta 90
_cell.angle_gamma 90
_symmetry.entry_id one
_symmetry.space_group_name_H-M 'P 1'
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.pdbx_PDB_ins_code
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
_atom_site.occupancy
_atom_site.B_iso_or_equiv
_atom_site.pdbx_formal_charge
_atom_site.auth_seq_id
_atom_site.auth_comp_id
_atom_site.auth_asym_id
_atom_site.auth_atom_id
_atom_site.pdbx_PDB_model_num
ATOM 1 C C . GLY A 1 1 ? 0.0 0.0 0.0 1.00 10.00 ? 1 GLY A C 1
"""
with open(os.path.join(root, "one.cif"), "w") as f:
    f.write(cif)
job = {
    "version": 1,
    "runtime": {"profile": "locked", "allow_paths": [root]},
    "inputs": {"input": {"path": os.path.join(root, "one.cif"), "format": "mmcif"}},
    "scene": {
        "canvas": {"background": "white"},
        "structures": [{
            "ref": "model",
            "source": "input",
            "components": [{
                "ref": "all",
                "select": "all",
                "representation": {"type": "spacefill", "color": "#cc3399"},
            }],
        }],
        "camera": {"focus": "all"},
    },
    "outputs": [{"type": "image", "path": os.path.join(root, "artifact.png"), "size": [64, 64]}],
}
with open(os.path.join(root, "artifact.job.json"), "w") as f:
    json.dump(job, f)
PY

run_with_timeout 120 molstar inspect "$WORKDIR/work/artifact.job.json" \
  --strict-semantic \
  --json >"$WORKDIR/inspect.json"
run_with_timeout 120 molstar bench "$WORKDIR/work/artifact.job.json" \
  --iterations 1 \
  --warmup 0 \
  --size 64x64 \
  --json >"$WORKDIR/bench.json"
run_with_timeout 30 molstar serve --openapi >"$WORKDIR/openapi.json"
run_with_timeout 180 molstar compat check --render --json >"$WORKDIR/compat.json"

SOCK="$WORKDIR/molstar.sock"
molstar serve \
  --socket "$SOCK" \
  --workers 1 \
  --job-store "$WORKDIR/server-jobs" \
  --prewarm \
  --quiet >"$WORKDIR/serve.stdout" 2>"$WORKDIR/serve.stderr" &
SERVE_PID=$!
for _ in $(seq 1 120); do
  if [ -S "$SOCK" ]; then
    break
  fi
  if ! kill -0 "$SERVE_PID" >/dev/null 2>&1; then
    cat "$WORKDIR/serve.stdout" >&2 || true
    cat "$WORKDIR/serve.stderr" >&2 || true
    exit 1
  fi
  sleep 1
done
if [ ! -S "$SOCK" ]; then
  cat "$WORKDIR/serve.stdout" >&2 || true
  cat "$WORKDIR/serve.stderr" >&2 || true
  echo "molstar serve did not create socket" >&2
  exit 1
fi
run_with_timeout 30 curl --fail --silent --unix-socket "$SOCK" http://unix/ready >"$WORKDIR/ready.json"
run_with_timeout 30 curl --fail --silent --unix-socket "$SOCK" http://unix/metrics >"$WORKDIR/metrics.json"
run_with_timeout 30 curl --fail --silent --unix-socket "$SOCK" http://unix/schema/openapi >"$WORKDIR/server-openapi.json"
run_with_timeout 60 molstar rpc capabilities --socket "$SOCK" --json >"$WORKDIR/rpc-capabilities.json"
run_with_timeout 60 molstar rpc metrics --socket "$SOCK" --json >"$WORKDIR/rpc-metrics.json"
run_with_timeout 60 molstar rpc explain "$WORKDIR/work/artifact.job.json" --socket "$SOCK" --json >"$WORKDIR/rpc-explain.json"
kill "$SERVE_PID" >/dev/null 2>&1 || true
wait "$SERVE_PID" >/dev/null 2>&1 || true
SERVE_PID=""

python3 - \
  "$WORKDIR/install.json" \
  "$WORKDIR/self-test.json" \
  "$WORKDIR/smoke.json" \
  "$WORKDIR/quickstart.json" \
  "$WORKDIR/inspect.json" \
  "$WORKDIR/bench.json" \
  "$WORKDIR/openapi.json" \
  "$WORKDIR/compat.json" \
  "$WORKDIR/ready.json" \
  "$WORKDIR/metrics.json" \
  "$WORKDIR/server-openapi.json" \
  "$WORKDIR/rpc-capabilities.json" \
  "$WORKDIR/rpc-metrics.json" \
  "$WORKDIR/rpc-explain.json" <<'PY'
import json
import os
import sys

with open(sys.argv[1]) as f:
    install = json.load(f)
runtime = install["home"]
for path in (
    "docs/cli/molstar.md",
    "completions/molstar.bash",
    "completions/_molstar",
    "completions/molstar.fish",
    "completions/molstar.ps1",
    "python/headlessmolstar/__init__.py",
):
    full = os.path.join(runtime, path)
    assert os.path.getsize(full) > 0, full

for path in sys.argv[1:]:
    with open(path) as f:
        data = json.load(f)
    if isinstance(data, dict) and data.get("jsonrpc") == "2.0":
        assert not data.get("error"), data
        result = data.get("result") or {}
        if isinstance(result, dict) and "ok" in result:
            assert result["ok"], data
        continue
    if isinstance(data, dict) and "ok" in data:
        assert data["ok"], data
    if path.endswith("quickstart.json"):
        assert data["render"]["ok"], data
        assert data["next"], data
    if path.endswith("smoke.json"):
        assert data["checks"], data
    if path.endswith("inspect.json"):
        assert data["semantic"]["ok"], data
    if path.endswith("openapi.json") or path.endswith("server-openapi.json"):
        assert data["openapi"].startswith("3."), data
PY

printf 'verified molstar artifact %s\n' "$ARTIFACT_PATH"
