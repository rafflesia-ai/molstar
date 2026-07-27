# Changelog

## Unreleased

### Added

- Initial repo, extracted from `headlessmolstar` as a dedicated home for the headless Mol\* renderer CLI.
- Go/Cobra `molstar` headless CLI for declarative render jobs, recipes, MVS export, dry-run planning, fixtures, benchmark snapshots, and server/RPC operation.
- Agent-oriented JSON error envelopes with stable `agent_code`, retryability, exit code, and diagnosis fields.
- Renderer capability matrix covering primary, fallback, validation, and worker targets.
- Docker runtime/render smoke profiles plus Docker packaged-artifact smoke for Linux release candidates.
- Release-candidate wrapper that produces an artifact copy, SHA-256 file, verification log, and manifest.
- OpenAPI code samples and Prometheus metrics for the long-lived `molstar serve` mode.
- JSON contract snapshot tests for agent-facing CLI/server responses.
- `molstar agent doctor --json` for automation startup checks across doctor, capabilities, schema validation, inspect, dry-run render, and OpenAPI.
- Dependency/security audit tooling with npm audit, optional `govulncheck`, Go/npm inventory, and license summary reports.
- Review grouping script for splitting large worktrees into coherent release, contracts, run logs, Docker/CI, generated asset, and audit review sets.
- Run-log asset policy flags for disabling embedded local inputs and capping per-input/total embedded bytes.

### Changed

- The Go module is now `github.com/rafflesia-ai/molstar`, matching the repo and the `molstar` binary.
- `mdsrv-headless`, the standalone structural-biology tool CLIs, and their docs, schemas, examples, scripts, and CI jobs stay in `headlessmolstar` and are no longer built, packaged, or referenced here.
- Runtime identifiers that read `headlessmolstar` (Prometheus metric names, the `/health` `service` field, the `headlessmolstar.agent-doctor/v1` contract, the `headlessmolstar-worker-v1` renderer protocol, XDG paths, the job schema filename, the npm and Python package names) are unchanged — they are published contract surface.
- The release gate now includes TypeScript typechecking and renderer JavaScript syntax checking.
- The manual release-candidate workflow now runs the same `scripts/release-candidate.sh` entry point documented for local use.
- Agent JSON contract tests now include compact stable-field assertions in addition to full shape snapshots.

### Notes

- Linux amd64 is the primary renderer release target. macOS and Windows are contract-supported, while full renderer support depends on native Node canvas/GL dependencies.
