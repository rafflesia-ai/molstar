#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DEFAULT_MOLSTAR=0
if [ -z "${MOLSTAR_BIN:-}" ]; then
  MOLSTAR_BIN="$ROOT/bin/molstar"
  BUILD_DEFAULT_MOLSTAR=1
fi

if [ "$BUILD_DEFAULT_MOLSTAR" = "1" ] || [ ! -x "$MOLSTAR_BIN" ]; then
  LDFLAGS="$("$ROOT/scripts/build-ldflags.sh")"
  mkdir -p "$(dirname "$MOLSTAR_BIN")"
  (cd "$ROOT" && go build -ldflags "$LDFLAGS" -o "$MOLSTAR_BIN" ./cmd/molstar)
fi

"$MOLSTAR_BIN" docs --out "$ROOT/docs/cli" >/dev/null
perl -0pi -e 's/\n+\z/\n/' "$ROOT"/docs/cli/*.md
"$MOLSTAR_BIN" completion all --out-dir "$ROOT/completions" >/dev/null

test -s "$ROOT/docs/cli/molstar.md"
test -s "$ROOT/completions/molstar.bash"
test -s "$ROOT/completions/_molstar"
test -s "$ROOT/completions/molstar.fish"
test -s "$ROOT/completions/molstar.ps1"

printf 'generated CLI docs and completions\n'
