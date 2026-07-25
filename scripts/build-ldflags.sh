#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MOLSTAR_PKG="github.com/sacha-ichbiah/molstar/internal/cli"
VERSION="$(cd "$ROOT" && node -p "require('./package.json').version" 2>/dev/null || printf 'dev')"
COMMIT="$(cd "$ROOT" && git rev-parse --short=12 HEAD 2>/dev/null || true)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

printf -- "-X %s.buildVersion=%s -X %s.buildCommit=%s -X %s.buildDate=%s" \
  "$MOLSTAR_PKG" "$VERSION" "$MOLSTAR_PKG" "$COMMIT" "$MOLSTAR_PKG" "$DATE"
