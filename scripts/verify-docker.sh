#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-molstar:local}"
DOCKER_BIN="${DOCKER_BIN:-docker}"
DOCKER_RUN_EXTRA_ARGS="${DOCKER_RUN_EXTRA_ARGS:-}"
DOCKER_VERIFY_PROFILE="${DOCKER_VERIFY_PROFILE:-runtime}"
case "$DOCKER_VERIFY_PROFILE" in
  runtime|render|auto)
    ;;
  *)
    echo "DOCKER_VERIFY_PROFILE must be runtime, render, or auto" >&2
    exit 2
    ;;
esac
WORKDIR="$(mktemp -d)"
SMOKE_STATUS="failed"
RENDER_AVAILABLE="unknown"
cleanup() {
  local status="$?"
  set +e
  python3 - "$WORKDIR/docker-smoke-summary.json" "$SMOKE_STATUS" "$DOCKER_VERIFY_PROFILE" "$IMAGE" "$RENDER_AVAILABLE" <<'PY'
import json
import sys
from datetime import datetime, timezone

path, status, profile, image, render_available = sys.argv[1:]
with open(path, "w") as f:
    json.dump({
        "ok": status == "passed",
        "status": status,
        "profile": profile,
        "image": image,
        "render_available": render_available,
        "finished_at": datetime.now(timezone.utc).isoformat(),
    }, f, indent=2)
    f.write("\n")
PY
  if [ -n "${DOCKER_VERIFY_OUT_DIR:-}" ]; then
    mkdir -p "$DOCKER_VERIFY_OUT_DIR"
    while IFS= read -r file; do
      rel="${file#$WORKDIR/}"
      mkdir -p "$DOCKER_VERIFY_OUT_DIR/$(dirname "$rel")"
      cp "$file" "$DOCKER_VERIFY_OUT_DIR/$rel"
    done < <(find "$WORKDIR" -type f)
  fi
  if [ "${DOCKER_VERIFY_KEEP_WORKDIR:-0}" = "1" ]; then
    echo "docker smoke artifacts kept at $WORKDIR" >&2
  else
    rm -rf "$WORKDIR"
  fi
  exit "$status"
}
trap cleanup EXIT

run_with_timeout() {
  local seconds="$1"
  shift
  local command=("$@")
  if [ "${command[0]:-}" = "docker_run" ]; then
    local docker_args=()
    if [ -n "$DOCKER_RUN_EXTRA_ARGS" ]; then
      # shellcheck disable=SC2206
      docker_args=($DOCKER_RUN_EXTRA_ARGS)
    fi
    local original=("${command[@]:1}")
    command=("$DOCKER_BIN" run)
    if [ "${#docker_args[@]}" -gt 0 ]; then
      command+=("${docker_args[@]}")
    fi
    command+=("${original[@]}")
  fi
  perl -e 'alarm shift @ARGV; exec @ARGV' "$seconds" "${command[@]}"
}

DOCKER_INFO_TIMEOUT_SECONDS="${DOCKER_INFO_TIMEOUT_SECONDS:-60}"
"$DOCKER_BIN" info >/dev/null 2>&1 &
DOCKER_INFO_PID="$!"
for _ in $(seq 1 "$DOCKER_INFO_TIMEOUT_SECONDS"); do
  if ! kill -0 "$DOCKER_INFO_PID" 2>/dev/null; then
    wait "$DOCKER_INFO_PID"
    DOCKER_INFO_STATUS="$?"
    if [ "$DOCKER_INFO_STATUS" -ne 0 ]; then
      echo "docker daemon is not reachable; start Docker and rerun npm run docker:verify:runtime" >&2
      exit "$DOCKER_INFO_STATUS"
    fi
    break
  fi
  sleep 1
done
if kill -0 "$DOCKER_INFO_PID" 2>/dev/null; then
  kill "$DOCKER_INFO_PID" 2>/dev/null || true
  wait "$DOCKER_INFO_PID" 2>/dev/null || true
  echo "docker daemon did not respond within ${DOCKER_INFO_TIMEOUT_SECONDS} seconds; start or restart Docker and rerun npm run docker:verify:runtime" >&2
  exit 124
fi

run_with_timeout 120 docker_run --rm --entrypoint molstar "$IMAGE" doctor --skip-probe --json >/dev/null
run_with_timeout 120 docker_run -i --rm --entrypoint python3 "$IMAGE" - <<'PY'
import sys
sys.path.insert(0, "/app/python")
from headlessmolstar import HeadlessMolstar

report = HeadlessMolstar(["molstar"]).version(runtime=False)
assert report["ok"], report
PY
run_with_timeout 120 docker_run --rm "$IMAGE" completion bash >/dev/null
run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  recipe init ligand --id 1cbs --out /work/ligand.recipe.yaml >/dev/null
run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  recipe validate /work/ligand.recipe.yaml --schema --json >/dev/null
run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  recipe explain /work/ligand.recipe.yaml --schema --json >/dev/null
run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  recipe compile /work/ligand.recipe.yaml --out /work/ligand.mvsj --json >/dev/null
run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  selectors explain 'chain:A/residue:10-20' --json >/dev/null
if ! run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
  capabilities --json --probe-worker >"$WORKDIR/molstar-capabilities.json"; then
  if [ "$DOCKER_VERIFY_PROFILE" = "render" ]; then
    echo "Docker render profile requires renderer capabilities to pass" >&2
    exit 1
  fi
  echo "warning: Docker renderer capabilities failed; continuing runtime profile checks" >&2
fi
RENDER_AVAILABLE="$(python3 - "$WORKDIR/molstar-capabilities.json" <<'PY'
import json
import sys

try:
    with open(sys.argv[1]) as f:
        report = json.load(f)
except Exception:
    print("0")
    raise SystemExit
matrix = report.get("matrix") or []
targets = {row.get("target") for row in matrix}
assert {"primary", "fallback", "validate", "worker"}.issubset(targets), report
worker = report.get("worker", {}).get("capabilities", {})
primary = report.get("renderer", {}).get("primary", {}).get("capabilities", {}).get("renderer", {})
gl = worker.get("gl") or primary.get("gl") or {}
print("1" if gl.get("available") else "0")
PY
)"
if [ "$DOCKER_VERIFY_PROFILE" = "render" ] && [ "$RENDER_AVAILABLE" != "1" ]; then
  echo "Docker render profile requires headless WebGL, but renderer capabilities reported unavailable" >&2
  cat "$WORKDIR/molstar-capabilities.json" >&2 || true
  exit 1
fi
if [ "$DOCKER_VERIFY_PROFILE" = "render" ] || { [ "$DOCKER_VERIFY_PROFILE" = "auto" ] && [ "$RENDER_AVAILABLE" = "1" ]; }; then
  run_with_timeout 180 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    fixtures verify --out-dir /work/molstar-fixtures --golden --json >"$WORKDIR/molstar-fixtures.json"
  python3 - "$WORKDIR/molstar-fixtures.json" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    report = json.load(f)
assert report["ok"], report
goldens = []
for fixture in report.get("fixtures", []):
    goldens.extend(fixture.get("golden", []))
assert goldens, report
for golden in goldens:
    assert golden["ok"], golden
    if golden["type"] == "image":
        assert "average_hash=" in golden.get("detail", ""), golden
PY
else
  echo "info: skipping Docker Molstar render checks in $DOCKER_VERIFY_PROFILE profile" >&2
fi
cat >"$WORKDIR/local.cif" <<'CIF'
data_one
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
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
_atom_site.occupancy
_atom_site.B_iso_or_equiv
ATOM 1 C C GLY A 1 1 0.0 0.0 0.0 1.0 10.0
CIF
cat >"$WORKDIR/local.job.yaml" <<'YAML'
version: 1
runtime:
  profile: locked
  strict: true
  allow_paths:
    - /work
inputs:
  input:
    path: /work/local.cif
    format: mmcif
scene:
  canvas:
    background: white
  structures:
    - source: input
      components:
        - ref: all
          select: all
          representation:
            type: spacefill
            color: "#cc3399"
  camera:
    focus: all
outputs:
  - type: image
    path: /work/server.png
    size: [64, 64]
YAML
cat >"$WORKDIR/server-export.job.yaml" <<'YAML'
version: 1
runtime:
  profile: locked
  strict: true
  allow_paths:
    - /work
inputs:
  input:
    path: /work/local.cif
    format: mmcif
scene:
  canvas:
    background: white
  structures:
    - source: input
      components:
        - ref: all
          select: all
          representation:
            type: spacefill
            color: "#cc3399"
  camera:
    focus: all
outputs:
  - type: mvsj
    path: /work/server-scene.mvsj
YAML
if [ "$DOCKER_VERIFY_PROFILE" = "render" ] || { [ "$DOCKER_VERIFY_PROFILE" = "auto" ] && [ "$RENDER_AVAILABLE" = "1" ]; }; then
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    inspect /work/local.job.yaml --semantic=auto --strict-semantic --json >"$WORKDIR/inspect.json"
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    bench /work/local.job.yaml --iterations 1 --warmup 0 --size 64x64 --json >/dev/null
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    render /work/local.job.yaml --out /work/docker-render.png --size 64x64 --profile locked --json >"$WORKDIR/docker-render.json"
else
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    inspect /work/local.job.yaml --semantic=false --json >"$WORKDIR/inspect.json"
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    bench /work/local.job.yaml --iterations 1 --warmup 0 --size 64x64 --dry-run --json >/dev/null 2>/dev/null
  run_with_timeout 120 docker_run --rm --entrypoint molstar -v "$WORKDIR:/work" "$IMAGE" \
    render /work/local.job.yaml --out /work/docker-render.png --size 64x64 --profile locked --dry-run --json >"$WORKDIR/docker-render.json" 2>/dev/null
fi
run_with_timeout 120 docker_run --rm --entrypoint bash -v "$WORKDIR:/work" "$IMAGE" -lc '
  set -euo pipefail
  molstar serve --socket /work/molstar.sock --auth-token secret --no-worker --quiet >/work/server.log 2>&1 &
  pid="$!"
  cleanup() { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
  trap cleanup EXIT
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [ -S /work/molstar.sock ] && break
    sleep 1
  done
  test -S /work/molstar.sock
  curl --fail --silent --unix-socket /work/molstar.sock http://unix/ready >/work/server-ready.json
  curl --fail --silent --unix-socket /work/molstar.sock -H "Authorization: Bearer secret" http://unix/metrics >/work/server-metrics.json
  curl --fail --silent --unix-socket /work/molstar.sock -H "Authorization: Bearer secret" http://unix/metrics/prometheus >/work/server-metrics.prom
  curl --fail --silent --unix-socket /work/molstar.sock -H "Authorization: Bearer secret" http://unix/schema/openapi >/work/server-openapi.json
  molstar rpc capabilities --socket /work/molstar.sock --token secret --json >/work/rpc-capabilities.json
  molstar rpc metrics --socket /work/molstar.sock --token secret --json >/work/rpc-metrics.json
  molstar server submit /work/server-export.job.yaml --socket /work/molstar.sock --token secret --json >/work/server-submit.json
  job_id="$(python3 -c "import json; print(json.load(open(\"/work/server-submit.json\"))[\"id\"])")"
  molstar server wait "$job_id" --socket /work/molstar.sock --token secret --timeout 30s --interval 100ms --download-outputs --out-dir /work/server-downloads --json >/work/server-wait.json
  test -s /work/server-downloads/server-scene.mvsj
  molstar server logs "$job_id" --socket /work/molstar.sock --token secret >/work/server-logs.txt
  molstar server logs "$job_id" --socket /work/molstar.sock --token secret --json >/work/server-logs.json
  molstar server status "$job_id" --socket /work/molstar.sock --token secret --json >/work/server-status.json
  molstar server events "$job_id" --socket /work/molstar.sock --token secret --json >/work/server-events.json
  molstar server cancel "$job_id" --socket /work/molstar.sock --token secret --json >/work/server-cancel.json
'
SMOKE_STATUS="passed"
