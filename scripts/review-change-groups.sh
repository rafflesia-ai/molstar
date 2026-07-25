#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FORMAT="${REVIEW_GROUPS_FORMAT:-text}"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/review-change-groups.sh [--text|--markdown|--json]

Groups the current dirty worktree into reviewable slices and prints suggested
git add commands for each slice.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --text)
      FORMAT="text"
      ;;
    --markdown)
      FORMAT="markdown"
      ;;
    --json)
      FORMAT="json"
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
  shift
done
cd "$ROOT"

python3 - "$FORMAT" <<'PY'
import json
import subprocess
import sys
from collections import defaultdict

fmt = sys.argv[1]
raw = subprocess.check_output(["git", "status", "--short", "--untracked-files=all"], text=True)

groups = {
    "release_rc": [],
    "json_contracts": [],
    "run_logs": [],
    "molstar": [],
    "package_release": [],
    "docker_ci": [],
    "docs_cli_assets": [],
    "dependency_audit": [],
    "other": [],
}

def classify(path):
    if path in {"scripts/release-candidate.sh", "docs/platform-support.md"} or path.startswith(".github/workflows/release"):
        return "release_rc"
    if path in {"scripts/package-local.sh", "scripts/verify-release.sh", "scripts/verify-release-docker.sh", "scripts/verify-linux-clean.sh"}:
        return "package_release"
    if "testdata/contracts" in path or path.endswith("json_contract_test.go") or path in {"docs/json-contracts.md"}:
        return "json_contracts"
    if "run_logs" in path or "molstar_logs" in path:
        return "run_logs"
    if path.startswith("internal/cli/") or path.startswith("cmd/molstar") or path.startswith("scripts/dogfood-molstar"):
        return "molstar"
    if path.startswith(".github/") or "docker" in path:
        return "docker_ci"
    if path.startswith("docs/cli/") or path.startswith("completions/") or path == "scripts/generate-cli-assets.sh":
        return "docs_cli_assets"
    if path in {"scripts/audit-deps.sh"} or "audit" in path or "license" in path:
        return "dependency_audit"
    return "other"

for line in raw.splitlines():
    if not line.strip():
        continue
    status = line[:2].strip() or line[:2]
    path = line[3:]
    if " -> " in path:
        path = path.split(" -> ", 1)[1]
    groups[classify(path)].append({"status": status, "path": path})

ordered = [
    ("release_rc", "Release candidate and platform confidence"),
    ("json_contracts", "Agent JSON contracts"),
    ("run_logs", "Run logs, replay, and bundles"),
    ("molstar", "Molstar CLI"),
    ("package_release", "Package and release verification"),
    ("docker_ci", "Docker and CI"),
    ("docs_cli_assets", "Generated docs and completions"),
    ("dependency_audit", "Dependency audit and licenses"),
    ("other", "Other"),
]

report = {
    "ok": True,
    "groups": [
        {
            "id": key,
            "title": title,
            "files": groups[key],
            "count": len(groups[key]),
            "suggested_git_add": ["git", "add", "--", *[item["path"] for item in groups[key]]],
        }
        for key, title in ordered
        if groups[key]
    ],
}

if fmt == "json":
    json.dump(report, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
elif fmt == "markdown":
    for group in report["groups"]:
        print(f"## {group['title']} ({group['count']})")
        print()
        print("```sh")
        print(" ".join(group["suggested_git_add"]))
        print("```")
        print()
        for item in group["files"]:
            print(f"- `{item['status']}` `{item['path']}`")
        print()
else:
    for group in report["groups"]:
        print(f"## {group['title']} ({group['count']})")
        print("suggested:\t" + " ".join(group["suggested_git_add"]))
        for item in group["files"]:
            print(f"{item['status']}\t{item['path']}")
        print()
PY
