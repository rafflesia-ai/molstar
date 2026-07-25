#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MOLSTAR_BIN="${MOLSTAR_BIN:-$ROOT/bin/molstar}"
RUN_ID="${MOLSTAR_RUN_ID:-}"
CI_ARTIFACT="${MOLSTAR_CI_ARTIFACT:-}"
OUT="${MOLSTAR_ISSUE_BUNDLE:-}"
OUT_DIR="${MOLSTAR_CI_OUT_DIR:-}"
RUNS_DIR="${MOLSTAR_RUNS_DIR:-}"
REDACT_PATHS=1
REDACT_ENV=1
REDACT_INPUTS=1

usage() {
  cat >&2 <<'USAGE'
usage: collect-molstar-ci-artifact.sh --run-id RUN_ID --ci-artifact DIR [--out issue.zip | --out-dir DIR] [--runs-dir DIR] [--keep-inputs] [--no-redact]

Writes:
  issue.zip
  issue.zip.json
  issue.zip.manifest.json
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --run-id)
      RUN_ID="${2:-}"
      shift 2
      ;;
    --ci-artifact)
      CI_ARTIFACT="${2:-}"
      shift 2
      ;;
    --out)
      OUT="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    --runs-dir)
      RUNS_DIR="${2:-}"
      shift 2
      ;;
    --keep-inputs)
      REDACT_INPUTS=0
      shift
      ;;
    --no-redact)
      REDACT_PATHS=0
      REDACT_ENV=0
      REDACT_INPUTS=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [ ! -x "$MOLSTAR_BIN" ]; then
  MOLSTAR_BIN="molstar"
fi

if [ -z "$RUN_ID" ]; then
  if [ -n "$RUNS_DIR" ]; then
    RUN_ID="$("$MOLSTAR_BIN" logs --last --dir "$RUNS_DIR" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])')"
  else
    RUN_ID="$("$MOLSTAR_BIN" logs --last --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])')"
  fi
fi

if [ -z "$CI_ARTIFACT" ]; then
  echo "--ci-artifact is required" >&2
  usage
  exit 2
fi

sanitize_id() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_'
}

if [ -z "$OUT" ]; then
  if [ -z "$OUT_DIR" ]; then
    OUT_DIR="$(dirname "$CI_ARTIFACT")"
  fi
  mkdir -p "$OUT_DIR"
  OUT="$OUT_DIR/molstar-$(sanitize_id "$RUN_ID").issue.zip"
fi

args=(diagnose "$RUN_ID" --ci-artifact "$CI_ARTIFACT" --bundle --out "$OUT" --json)
if [ -n "$RUNS_DIR" ]; then
  args+=(--dir "$RUNS_DIR")
fi
if [ "$REDACT_PATHS" = "1" ]; then
  args+=(--redact-paths)
fi
if [ "$REDACT_ENV" = "1" ]; then
  args+=(--redact-env)
fi
if [ "$REDACT_INPUTS" = "1" ]; then
  args+=(--redact-inputs)
fi

mkdir -p "$(dirname "$OUT")"
"$MOLSTAR_BIN" "${args[@]}" >"$OUT.json"
test -s "$OUT"
test -s "$OUT.json"
python3 - "$OUT" "$OUT.json" "$RUN_ID" "$CI_ARTIFACT" "$RUNS_DIR" "$REDACT_PATHS" "$REDACT_ENV" "$REDACT_INPUTS" <<'PY'
import json
import pathlib
import sys
from datetime import datetime, timezone

bundle, report, run_id, ci_artifact, runs_dir, redact_paths, redact_env, redact_inputs = sys.argv[1:]
manifest = {
    "schema": "headlessmolstar.ci_issue_bundle/v1",
    "ok": True,
    "run_id": run_id,
    "issue_bundle": bundle,
    "diagnose_report": report,
    "ci_artifact": ci_artifact,
    "runs_dir": runs_dir or None,
    "redactions": {
        "paths": redact_paths == "1",
        "env": redact_env == "1",
        "inputs": redact_inputs == "1",
    },
    "generated_at": datetime.now(timezone.utc).isoformat(),
    "retention_hint": "Upload issue_bundle, diagnose_report, and this manifest as one CI artifact.",
}
pathlib.Path(bundle + ".manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
PY

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    printf '### Molstar issue bundle\n\n'
    printf -- '- Run ID: `%s`\n' "$RUN_ID"
    printf -- '- Bundle: `%s`\n' "$OUT"
    printf -- '- Diagnose report: `%s`\n' "$OUT.json"
    printf -- '- Manifest: `%s`\n' "$OUT.manifest.json"
    printf -- '- Redactions: paths=%s env=%s inputs=%s\n' "$REDACT_PATHS" "$REDACT_ENV" "$REDACT_INPUTS"
  } >>"$GITHUB_STEP_SUMMARY"
fi
printf '%s\n' "$OUT"
