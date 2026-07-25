#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

RUN_DOCKER="${RC_DOCKER:-0}"
RUN_RENDER_DOCKER="${RC_RENDER_DOCKER:-0}"
SKIP_GATES="${RC_SKIP_GATES:-0}"
REUSE_VERIFIED_ARTIFACT="${RC_REUSE_VERIFIED_ARTIFACT:-0}"
OUT_DIR="${RC_OUT_DIR:-$ROOT/dist/release-candidates}"
LABEL="${RC_LABEL:-}"
ARTIFACT_OVERRIDE="${RC_ARTIFACT:-}"

usage() {
  cat <<'EOF'
usage: scripts/release-candidate.sh [options]

Options:
  --docker          Build the Docker image and run runtime + artifact smoke.
  --render-docker  Also run the strict Docker render smoke; implies --docker.
  --skip-gates     Skip scripts/verify-release.sh and only package/verify artifact.
  --reuse-verified-artifact
                    Copy an existing, already verified artifact into the RC folder.
  --artifact PATH   Artifact to reuse with --reuse-verified-artifact.
  --label NAME     Use NAME for the release-candidate folder and artifact copy.
  -h, --help       Show this help.

Environment:
  RC_OUT_DIR       Output directory for RC manifests and smoke reports.
  RC_LABEL         Default label when --label is omitted.
  RC_ARTIFACT      Artifact path used by --reuse-verified-artifact.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --docker)
      RUN_DOCKER=1
      ;;
    --render-docker)
      RUN_DOCKER=1
      RUN_RENDER_DOCKER=1
      ;;
    --skip-gates)
      SKIP_GATES=1
      ;;
    --reuse-verified-artifact)
      REUSE_VERIFIED_ARTIFACT=1
      ;;
    --artifact)
      shift
      if [ "$#" -eq 0 ]; then
        echo "--artifact requires a value" >&2
        exit 2
      fi
      ARTIFACT_OVERRIDE="$1"
      ;;
    --label)
      shift
      if [ "$#" -eq 0 ]; then
        echo "--label requires a value" >&2
        exit 2
      fi
      LABEL="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

cd "$ROOT"
source "$ROOT/scripts/verify-artifact-lib.sh"

if [ "$REUSE_VERIFIED_ARTIFACT" = "1" ] && [ "$SKIP_GATES" = "1" ]; then
  echo "--reuse-verified-artifact and --skip-gates are mutually exclusive" >&2
  exit 2
fi
if [ -n "$ARTIFACT_OVERRIDE" ] && [ "$REUSE_VERIFIED_ARTIFACT" != "1" ]; then
  echo "--artifact is only valid with --reuse-verified-artifact" >&2
  exit 2
fi

short_commit="$(git rev-parse --short HEAD 2>/dev/null || printf 'nogit')"
created_id="$(date -u +%Y%m%dT%H%M%SZ)"
if [ -z "$LABEL" ]; then
  LABEL="rc-${created_id}-${short_commit}"
fi
SAFE_LABEL="$(printf '%s' "$LABEL" | tr -c 'A-Za-z0-9_.-' '-')"
if [ "$SAFE_LABEL" != "$LABEL" ]; then
  echo "release candidate label contained unsupported characters; using $SAFE_LABEL" >&2
  LABEL="$SAFE_LABEL"
fi

GOOS_VALUE="$(go env GOOS)"
GOARCH_VALUE="$(go env GOARCH)"
RC_DIR="$OUT_DIR/$LABEL"
mkdir -p "$RC_DIR"
LOG_PATH="$RC_DIR/release-candidate.log"
: >"$LOG_PATH"

run_step() {
  printf '==> %s\n' "$*" | tee -a "$LOG_PATH"
  "$@" 2>&1 | tee -a "$LOG_PATH"
}

release_gate="skipped"
artifact_verification="skipped"
if [ "$REUSE_VERIFIED_ARTIFACT" = "1" ]; then
  if [ -n "$ARTIFACT_OVERRIDE" ]; then
    ARTIFACT_PATH="$(ARTIFACT="$ARTIFACT_OVERRIDE" resolve_artifact "$ARTIFACT_OVERRIDE")"
  else
    ARTIFACT_PATH="$(resolve_artifact)"
  fi
  release_gate="reused"
  artifact_verification="reused"
elif [ "$SKIP_GATES" = "1" ]; then
  run_step npm run package:local
  ARTIFACT_PATH="$(resolve_artifact)"
  run_step env ARTIFACT="$ARTIFACT_PATH" "$ROOT/scripts/verify-artifact.sh"
  artifact_verification="passed"
else
  run_step "$ROOT/scripts/verify-release.sh" --skip-docker
  ARTIFACT_PATH="$(resolve_artifact)"
  release_gate="passed"
  artifact_verification="passed"
fi

case "$ARTIFACT_PATH" in
  *.tar.gz) artifact_ext=".tar.gz" ;;
  *.zip) artifact_ext=".zip" ;;
  *)
    echo "unsupported artifact extension: $ARTIFACT_PATH" >&2
    exit 2
    ;;
esac

RC_ARTIFACT="$RC_DIR/headlessmolstar-${LABEL}-${GOOS_VALUE}-${GOARCH_VALUE}${artifact_ext}"
cp "$ARTIFACT_PATH" "$RC_ARTIFACT"
python3 - "$RC_ARTIFACT" >"$RC_ARTIFACT.sha256" <<'PY'
import hashlib
import os
import sys

path = sys.argv[1]
digest = hashlib.sha256()
with open(path, "rb") as handle:
    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
        digest.update(chunk)
print(f"{digest.hexdigest()}  {os.path.basename(path)}")
PY

docker_runtime="skipped"
docker_artifact="skipped"
docker_render="skipped"
if [ "$RUN_DOCKER" = "1" ]; then
  if [ "$GOOS_VALUE" != "linux" ]; then
    echo "release candidate Docker artifact smoke requires a Linux artifact; current artifact is ${GOOS_VALUE}/${GOARCH_VALUE}" >&2
    echo "Run this command on Linux CI, or omit --docker for a local platform RC artifact." >&2
    exit 2
  fi
  run_step docker build -t headlessmolstar:rc .
  mkdir -p "$RC_DIR/docker-runtime" "$RC_DIR/docker-artifact"
  run_step env DOCKER_VERIFY_PROFILE=runtime DOCKER_VERIFY_OUT_DIR="$RC_DIR/docker-runtime" IMAGE=headlessmolstar:rc "$ROOT/scripts/verify-docker.sh"
  docker_runtime="passed"
  run_step env ARTIFACT="$RC_ARTIFACT" DOCKER_VERIFY_OUT_DIR="$RC_DIR/docker-artifact" IMAGE=headlessmolstar:rc "$ROOT/scripts/verify-docker-artifact.sh"
  docker_artifact="passed"
  if [ "$RUN_RENDER_DOCKER" = "1" ]; then
    mkdir -p "$RC_DIR/docker-render"
    run_step env DOCKER_VERIFY_PROFILE=render DOCKER_VERIFY_OUT_DIR="$RC_DIR/docker-render" IMAGE=headlessmolstar:rc "$ROOT/scripts/verify-docker.sh"
    docker_render="passed"
  fi
fi

git_commit="$(git rev-parse HEAD 2>/dev/null || true)"
git_dirty="false"
if [ -n "$(git status --porcelain 2>/dev/null || true)" ]; then
  git_dirty="true"
fi

python3 - \
  "$ROOT" \
  "$RC_DIR" \
  "$LABEL" \
  "$created_id" \
  "$git_commit" \
  "$git_dirty" \
  "$GOOS_VALUE" \
  "$GOARCH_VALUE" \
  "$RC_ARTIFACT" \
  "$release_gate" \
  "$artifact_verification" \
  "$docker_runtime" \
  "$docker_artifact" \
  "$docker_render" \
  "$LOG_PATH" \
  >"$RC_DIR/manifest.json" <<'PY'
import hashlib
import json
import os
import sys

(
    root,
    rc_dir,
    label,
    created_at,
    git_commit,
    git_dirty,
    goos,
    goarch,
    artifact_path,
    release_gate,
    artifact_verification,
    docker_runtime,
    docker_artifact,
    docker_render,
    log_path,
) = sys.argv[1:]

digest = hashlib.sha256()
with open(artifact_path, "rb") as handle:
    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
        digest.update(chunk)

def rel(path):
    relpath = os.path.relpath(path, root)
    if relpath == os.pardir or relpath.startswith(os.pardir + os.sep):
        return os.path.abspath(path)
    return relpath

manifest = {
    "ok": True,
    "kind": "headlessmolstar.release_candidate/v1",
    "label": label,
    "created_at": created_at,
    "git": {
        "commit": git_commit,
        "dirty": git_dirty == "true",
    },
    "platform": {
        "goos": goos,
        "goarch": goarch,
    },
    "artifact": {
        "path": rel(artifact_path),
        "name": os.path.basename(artifact_path),
        "bytes": os.path.getsize(artifact_path),
        "sha256": digest.hexdigest(),
        "sha256_file": rel(artifact_path + ".sha256"),
    },
    "checks": {
        "release_gate": release_gate,
        "artifact_verification": artifact_verification,
        "docker_runtime": docker_runtime,
        "docker_artifact": docker_artifact,
        "docker_render": docker_render,
    },
    "reports": {
        "log": rel(log_path),
        "docker_runtime": rel(os.path.join(rc_dir, "docker-runtime")),
        "docker_artifact": rel(os.path.join(rc_dir, "docker-artifact")),
        "docker_render": rel(os.path.join(rc_dir, "docker-render")),
    },
}
json.dump(manifest, sys.stdout, indent=2, sort_keys=True)
sys.stdout.write("\n")
PY

printf '%s\n' "$RC_DIR/manifest.json"
