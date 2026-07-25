#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_BIN="${DOCKER_BIN:-docker}"
IMAGE="${DEV_TEST_IMAGE:-headlessmolstar:dev-test}"
BUILD="${DEV_TEST_BUILD:-1}"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/dev-test.sh [--no-build] [--] [command...]

Builds and runs the hermetic Linux dev-test image. With no command it opens a shell.

Environment:
  DEV_TEST_IMAGE      image tag (default: headlessmolstar:dev-test)
  DEV_TEST_BUILD      set to 0 to skip docker build
  DOCKER_BIN          docker executable (default: docker)
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --no-build)
      BUILD=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

if [ "$BUILD" = "1" ]; then
  "$DOCKER_BIN" build -f "$ROOT/Dockerfile.dev-test" -t "$IMAGE" "$ROOT"
fi

if [ "$#" -eq 0 ]; then
  set -- bash
fi

run_args=(--rm)
if [ -t 0 ] && [ -t 1 ]; then
  run_args+=(-it)
fi

"$DOCKER_BIN" run "${run_args[@]}" "$IMAGE" "$@"
