#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

MOLSTAR_BIN="${MOLSTAR_BIN:-$WORKDIR/molstar}"
if [ ! -x "$MOLSTAR_BIN" ]; then
  LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
  (cd "$ROOT" && go build -ldflags "$LDFLAGS" -o "$MOLSTAR_BIN" ./cmd/molstar)
fi

"$MOLSTAR_BIN" docs --out "$WORKDIR/docs/cli" >/dev/null
"$MOLSTAR_BIN" completion all --out-dir "$WORKDIR/completions" >/dev/null
"$MOLSTAR_BIN" job schema --out "$WORKDIR/headlessmolstar-job-v1.schema.json" >/dev/null

diff -ru "$ROOT/docs/cli" "$WORKDIR/docs/cli"
diff -ru "$ROOT/completions" "$WORKDIR/completions"
diff -u "$ROOT/schema/headlessmolstar-job-v1.schema.json" "$WORKDIR/headlessmolstar-job-v1.schema.json"

printf 'generated docs, completions, and schemas are current\n'
