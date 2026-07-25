#!/usr/bin/env bash
set -euo pipefail

workspace="${HEADLESSMOLSTAR_WORKSPACE:-/workspace}"
config="${MOLSTAR_CONFIG:-/tmp/headlessmolstar-dev-test-config.json}"
node_bin="$workspace/node_modules/node/bin/node"
if [ ! -x "$node_bin" ]; then
  node_bin="node"
fi

mkdir -p "$(dirname "$config")"
cat >"$config" <<JSON
{
  "home": "$workspace",
  "renderer_command": ["$node_bin", "$workspace/scripts/render-mvs.js"],
  "renderer_fallback_command": ["$node_bin", "$workspace/scripts/molstar-node-cli.js", "$workspace/node_modules/.bin/mvs-render"],
  "validate_command": ["$node_bin", "$workspace/scripts/molstar-node-cli.js", "$workspace/node_modules/.bin/mvs-validate"]
}
JSON

exec "$@"
