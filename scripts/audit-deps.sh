#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${AUDIT_OUT:-}"
NPM_AUDIT_LEVEL="${NPM_AUDIT_LEVEL:-high}"

cd "$ROOT"

WORKDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

npm_status="skipped"
go_vuln_status="skipped"
licenses_status="passed"

if [ "${AUDIT_SKIP_NPM:-0}" != "1" ]; then
  if npm audit --audit-level="$NPM_AUDIT_LEVEL" --json >"$WORKDIR/npm-audit.json"; then
    npm_status="passed"
  else
    npm_status="failed"
  fi
else
  printf '{"ok":true,"skipped":true}\n' >"$WORKDIR/npm-audit.json"
fi

go list -m -json all >"$WORKDIR/go-modules.jsonl"
npm ls --all --json >"$WORKDIR/npm-tree.json" 2>"$WORKDIR/npm-tree.stderr" || true

if command -v govulncheck >/dev/null 2>&1; then
  if govulncheck -json ./... >"$WORKDIR/govulncheck.jsonl"; then
    go_vuln_status="passed"
  else
    go_vuln_status="failed"
  fi
else
  printf '{"skipped":true,"reason":"govulncheck not installed"}\n' >"$WORKDIR/govulncheck.jsonl"
fi

python3 - "$WORKDIR" "$ROOT" >"$WORKDIR/license-summary.json" <<'PY'
import json
import os
import sys

workdir, root = sys.argv[1:3]

def load_json(path):
    with open(path) as f:
        return json.load(f)

def walk_npm(node, out):
    deps = node.get("dependencies") or {}
    for name, dep in sorted(deps.items()):
        version = dep.get("version", "")
        license_value = dep.get("license") or dep.get("licenses") or ""
        out.append({"ecosystem": "npm", "name": name, "version": version, "license": license_value})
        walk_npm(dep, out)

packages = []
try:
    walk_npm(load_json(os.path.join(workdir, "npm-tree.json")), packages)
except Exception as exc:
    packages.append({"ecosystem": "npm", "error": str(exc)})

with open(os.path.join(workdir, "go-modules.jsonl")) as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            module = json.loads(line)
        except Exception:
            continue
        packages.append({
            "ecosystem": "go",
            "name": module.get("Path", ""),
            "version": module.get("Version", ""),
            "license": "",
        })

seen = set()
deduped = []
for package in packages:
    key = (package.get("ecosystem"), package.get("name"), package.get("version"))
    if key in seen:
        continue
    seen.add(key)
    deduped.append(package)

json.dump({"ok": True, "packages": deduped, "count": len(deduped)}, sys.stdout, indent=2, sort_keys=True)
sys.stdout.write("\n")
PY

python3 - "$WORKDIR" "$npm_status" "$go_vuln_status" "$licenses_status" >"$WORKDIR/audit-summary.json" <<'PY'
import json
import os
import sys

workdir, npm_status, go_vuln_status, licenses_status = sys.argv[1:5]

summary = {
    "ok": npm_status != "failed" and go_vuln_status != "failed" and licenses_status != "failed",
    "npm_audit": npm_status,
    "go_vulncheck": go_vuln_status,
    "licenses": licenses_status,
    "reports": {
        "npm_audit": "npm-audit.json",
        "go_vulncheck": "govulncheck.jsonl",
        "go_modules": "go-modules.jsonl",
        "npm_tree": "npm-tree.json",
        "licenses": "license-summary.json",
    },
}
json.dump(summary, sys.stdout, indent=2, sort_keys=True)
sys.stdout.write("\n")
PY

if [ -n "$OUT" ]; then
  mkdir -p "$OUT"
  cp "$WORKDIR"/* "$OUT"/
  cat "$OUT/audit-summary.json"
else
  cat "$WORKDIR/audit-summary.json"
fi

if [ "$npm_status" = "failed" ] || [ "$go_vuln_status" = "failed" ]; then
  exit 1
fi
