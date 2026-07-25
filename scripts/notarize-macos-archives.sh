#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "macOS notarization must run on macOS" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IDENTITY="${APPLE_DEVELOPER_IDENTITY:-}"
if [ -z "$IDENTITY" ]; then
  echo "APPLE_DEVELOPER_IDENTITY is required" >&2
  exit 2
fi

notary_args=()
if [ -n "${APPLE_NOTARY_PROFILE:-}" ]; then
  notary_args=(--keychain-profile "$APPLE_NOTARY_PROFILE")
else
  if [ -z "${APPLE_ID:-}" ] || [ -z "${APPLE_TEAM_ID:-}" ] || [ -z "${APPLE_APP_SPECIFIC_PASSWORD:-}" ]; then
    echo "set APPLE_NOTARY_PROFILE or APPLE_ID, APPLE_TEAM_ID, and APPLE_APP_SPECIFIC_PASSWORD" >&2
    exit 2
  fi
  notary_args=(--apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD")
fi

if [ "$#" -eq 0 ]; then
  set -- "$ROOT"/dist/*_darwin_*.zip
fi

for archive in "$@"; do
  if [ ! -f "$archive" ]; then
    echo "archive not found: $archive" >&2
    exit 2
  fi
  case "$archive" in
    *.zip) ;;
    *) echo "notarization expects a zip archive: $archive" >&2; exit 2 ;;
  esac

  workdir="$(mktemp -d)"

  extract_dir="$workdir/extracted"
  mkdir -p "$extract_dir"
  ditto -x -k "$archive" "$extract_dir"

  signed=0
  while IFS= read -r -d '' binary; do
    codesign --force --timestamp --options runtime --sign "$IDENTITY" "$binary"
    codesign --verify --strict --verbose=2 "$binary"
    signed=$((signed + 1))
  done < <(find "$extract_dir" -type f -name molstar -perm -111 -print0)

  if [ "$signed" -eq 0 ]; then
    echo "no CLI binaries found to sign in $archive" >&2
    exit 1
  fi

  tmp_archive="$workdir/$(basename "$archive")"
  (cd "$extract_dir" && ditto -c -k --sequesterRsrc . "$tmp_archive")
  mv "$tmp_archive" "$archive"

  xcrun notarytool submit "$archive" --wait "${notary_args[@]}" | tee "$archive.notary.log"
  printf 'signed and notarized %s\n' "$archive"
  rm -rf "$workdir"
done
