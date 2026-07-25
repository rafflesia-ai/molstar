#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/verify-artifact-lib.sh"

IMAGE="${IMAGE:-headlessmolstar:local}"
ARTIFACT_PATH="$(resolve_artifact "${1:-}")"
case "$(basename "$ARTIFACT_PATH")" in
  *linux*)
    ;;
  *)
    echo "Docker artifact smoke requires a Linux artifact; got $(basename "$ARTIFACT_PATH"). Run this on Linux CI or pass ARTIFACT=/path/to/headlessmolstar-*-linux-*.tar.gz." >&2
    exit 2
    ;;
esac
WORKDIR="$(mktemp -d)"
STATUS="failed"
cleanup() {
  local status="$?"
  set +e
  python3 - "$WORKDIR/docker-artifact-summary.json" "$STATUS" "$IMAGE" "$(basename "$ARTIFACT_PATH")" <<'PY'
import json
import sys
from datetime import datetime, timezone

path, status, image, artifact = sys.argv[1:]
with open(path, "w") as f:
    json.dump({
        "ok": status == "passed",
        "status": status,
        "image": image,
        "artifact": artifact,
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
  rm -rf "$WORKDIR"
  exit "$status"
}
trap cleanup EXIT

ARTIFACT_NAME="$(basename "$ARTIFACT_PATH")"
cp "$ARTIFACT_PATH" "$WORKDIR/$ARTIFACT_NAME"

docker run --rm --entrypoint bash -v "$WORKDIR:/work" -e ARTIFACT_NAME="$ARTIFACT_NAME" "$IMAGE" -lc '
set -euo pipefail
molstar install-artifact \
  --artifact "/work/$ARTIFACT_NAME" \
  --prefix /work/runtime \
  --bin-dir /work/bin \
  --config /work/config.json \
  --force \
  --json >/work/install.json
export PATH="/work/bin:$PATH"
export MOLSTAR_CONFIG=/work/config.json
molstar doctor --skip-probe --json >/work/doctor.json
molstar capabilities --json --probe-worker >/work/capabilities.json
molstar render --demo --out /work/artifact-demo.png --size 96x72 --json >/work/render.json
molstar fixtures verify --out-dir /work/fixtures --golden --json >/work/fixtures.json
python3 - <<PY
import json
import os
import sys

for path in ("/work/install.json", "/work/doctor.json", "/work/capabilities.json", "/work/render.json", "/work/fixtures.json"):
    with open(path) as f:
        data = json.load(f)
    assert data.get("ok"), (path, data)
assert os.path.getsize("/work/artifact-demo.png") > 0
with open("/work/fixtures.json") as f:
    fixtures = json.load(f)
goldens = [entry for fixture in fixtures["fixtures"] for entry in fixture.get("golden", [])]
assert goldens, fixtures
for entry in goldens:
    assert entry["ok"], entry
    if entry["type"] == "image":
        assert "average_hash=" in entry.get("detail", ""), entry
PY
'

STATUS="passed"
