#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_BIN="${DOCKER_BIN:-docker}"
DEV_TEST_IMAGE="${DEV_TEST_IMAGE:-headlessmolstar:dev-test}"
RUNTIME_IMAGE="${RUNTIME_IMAGE:-headlessmolstar:release-check}"
OUT_DIR="${VERIFY_RELEASE_DOCKER_OUT_DIR:-$ROOT/dist/verify-release-docker}"
BUILD_DEV_TEST="${VERIFY_RELEASE_DOCKER_BUILD:-1}"
NO_CACHE="${VERIFY_RELEASE_DOCKER_NO_CACHE:-0}"
RUN_RETRIES="${VERIFY_RELEASE_DOCKER_RUN_RETRIES:-1}"
GO_CACHE="${VERIFY_RELEASE_DOCKER_GO_CACHE:-0}"
GO_PARALLELISM="${VERIFY_RELEASE_DOCKER_GO_PARALLELISM:-${VERIFY_RELEASE_GO_PARALLELISM:-2}}"
VERIFY_RUNTIME_DOCKER=0
VERIFY_RUNTIME_DOCKER_RENDER=0

usage() {
  cat >&2 <<'USAGE'
usage: scripts/verify-release-docker.sh [--docker] [--render-docker] [--skip-runtime-docker]

Runs the release verifier inside the hermetic dev-test Linux image. Optional
runtime Docker checks run outside the dev-test container so they can use the
host Docker daemon.

Environment:
  DEV_TEST_IMAGE                 dev-test image tag
  RUNTIME_IMAGE                  runtime image tag for optional Docker smoke
  VERIFY_RELEASE_DOCKER_OUT_DIR  host output directory for reports
  VERIFY_RELEASE_DOCKER_BUILD    set to 0 to skip building the dev-test image
  VERIFY_RELEASE_DOCKER_NO_CACHE set to 1 to rebuild Docker images from scratch
  VERIFY_RELEASE_DOCKER_RUN_RETRIES retry dev-test docker run on Docker infra failures
  VERIFY_RELEASE_DOCKER_GO_CACHE set to 1 to mount persistent Go cache volumes
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --docker)
      VERIFY_RUNTIME_DOCKER=1
      ;;
    --render-docker)
      VERIFY_RUNTIME_DOCKER=1
      VERIFY_RUNTIME_DOCKER_RENDER=1
      ;;
    --skip-runtime-docker|--skip-docker)
      VERIFY_RUNTIME_DOCKER=0
      VERIFY_RUNTIME_DOCKER_RENDER=0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      usage
      exit 2
      ;;
  esac
done

mkdir -p "$OUT_DIR"

if [ "$BUILD_DEV_TEST" = "1" ]; then
  build_args=(-f "$ROOT/Dockerfile.dev-test" -t "$DEV_TEST_IMAGE")
  if [ "$NO_CACHE" = "1" ]; then
    build_args+=(--no-cache --pull)
  fi
  "$DOCKER_BIN" build "${build_args[@]}" "$ROOT"
fi

release_run_args=(
  --rm
  -v "$OUT_DIR:/artifacts"
  -e VERIFY_RELEASE_REPORT=/artifacts/verify-release-report.json
  -e VERIFY_RELEASE_STEP_LOG=/artifacts/verify-release-steps.jsonl
  -e VERIFY_RELEASE_OUT_DIR=/artifacts/out
  -e VERIFY_RELEASE_DIST_DIR=/artifacts/package-dist
  -e VERIFY_RELEASE_GO_PARALLELISM="$GO_PARALLELISM"
  -e PACKAGE_DIST_DIR=/artifacts/package-dist
  -e PACKAGE_LOG_DIR=/artifacts/package-logs
  -e PACKAGE_REPORT=/artifacts/package-logs/package-report.json
  -e PACKAGE_STEP_LOG=/artifacts/package-logs/package-steps.jsonl
)
if [ "$GO_CACHE" = "1" ]; then
  release_run_args+=(
    -v "${VERIFY_RELEASE_DOCKER_GOMODCACHE_VOLUME:-headlessmolstar-release-gomod}:/go/pkg/mod"
    -v "${VERIFY_RELEASE_DOCKER_GOCACHE_VOLUME:-headlessmolstar-release-gocache}:/root/.cache/go-build"
  )
fi

release_attempt=1
while true; do
  set +e
  "$DOCKER_BIN" run "${release_run_args[@]}" "$DEV_TEST_IMAGE" ./scripts/verify-release.sh --skip-docker
  release_code="$?"
  set -e
  if [ "$release_code" -eq 0 ]; then
    break
  fi
  if [ "$release_attempt" -ge "$RUN_RETRIES" ]; then
    exit "$release_code"
  fi
  echo "dev-test release verifier failed with exit $release_code; retrying ($release_attempt/$RUN_RETRIES)" >&2
  sleep "$((release_attempt * 5))"
  release_attempt="$((release_attempt + 1))"
done

runtime_ok=true
if [ "$VERIFY_RUNTIME_DOCKER" = "1" ]; then
  runtime_build_args=(-t "$RUNTIME_IMAGE")
  if [ "$NO_CACHE" = "1" ]; then
    runtime_build_args+=(--no-cache --pull)
  fi
  "$DOCKER_BIN" build "${runtime_build_args[@]}" "$ROOT"
  render_profile="$VERIFY_RUNTIME_DOCKER_RENDER"
  DOGFOOD_DOCKER_BUILD=0 \
    DOGFOOD_DOCKER_RENDER="$render_profile" \
    DOGFOOD_DOCKER_OUT_DIR="$OUT_DIR/docker-smoke" \
    IMAGE="$RUNTIME_IMAGE" \
    DOCKER_BIN="$DOCKER_BIN" \
    "$ROOT/scripts/dogfood-molstar-docker.sh"
else
  runtime_ok=false
fi

python3 - "$OUT_DIR" "$DEV_TEST_IMAGE" "$RUNTIME_IMAGE" "$VERIFY_RUNTIME_DOCKER" "$VERIFY_RUNTIME_DOCKER_RENDER" <<'PY'
import json
import pathlib
import sys
from datetime import datetime, timezone

out_dir = pathlib.Path(sys.argv[1])
report_path = out_dir / "verify-release-report.json"
release = {}
if report_path.exists():
    release = json.loads(report_path.read_text())
summary = {
    "ok": bool(release.get("ok")) and sys.argv[4] in {"0", "1"},
    "dev_test_image": sys.argv[2],
    "runtime_image": sys.argv[3],
    "runtime_docker": sys.argv[4] == "1",
    "runtime_docker_render": sys.argv[5] == "1",
    "release": release,
    "generated_at": datetime.now(timezone.utc).isoformat(),
}
if summary["runtime_docker"]:
    docker_reports = {}
    for path in sorted((out_dir / "docker-smoke").glob("*/docker-smoke-summary.json")):
        docker_reports[path.parent.name] = json.loads(path.read_text())
    summary["docker_reports"] = docker_reports
    summary["ok"] = summary["ok"] and bool(docker_reports) and all(item.get("ok") for item in docker_reports.values())
(out_dir / "verify-release-docker-summary.json").write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
print(json.dumps(summary, indent=2, sort_keys=True))
raise SystemExit(0 if summary["ok"] else 1)
PY
