# Claude Instructions

- Store expensive generated/runtime artifacts under `/Volumes/ExtremeSSD/molstar/`.
- Use repo-local symlinks only when tools require the original path. `node_modules`, `bin`, `dist`,
  `outputs`, `.molstar-cache`, and `.molstar-runs` are all symlinks into that directory.
- If `node_modules` is missing or reads as zeros (external SSD not mounted, or a bad copy), recreate
  it: `rm -rf /Volumes/ExtremeSSD/molstar/node_modules && npm install`. It is fully reproducible from
  `package-lock.json` — never try to repair it in place. `gl` and `canvas` compile natively, so this
  is slow.

## Git workflow

- Single-committer play repo — **no branches, no pull requests**.
- Commit and push directly to `main`. This overrides the usual "branch before committing to the default branch" default; stay on `main` and push there.

## Repo scope

This repo is the headless Mol\* renderer CLI only. It was carved out of `headlessmolstar`, which
retains `mdsrv-headless` and the standalone structural-biology tool CLIs — don't add those back here.

Identifiers reading `headlessmolstar` (Prometheus metric names, the `/health` `service` field, the
`headlessmolstar.agent-doctor/v1` contract, the `headlessmolstar-worker-v1` renderer protocol, XDG
config/data paths, the job schema filename, the npm package name, the Python package name) are
**published contract surface and are deliberately not renamed**. The npm package in particular cannot
be called `molstar`, because it depends on the upstream `molstar` package. The binary is `molstar`;
the Go module is `github.com/sacha-ichbiah/molstar`.
