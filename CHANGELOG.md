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

### Fixed

- `molstar fixtures verify --network` passes again. The three public recipe fixtures set `--out` and `--size` but not `sizeExplicit`, so the recipes' declared 1200x900 output size won and each fixture's own output verification then rejected the render for having the wrong dimensions.
- The `surface` preset focuses the polymer instead of the ligand. A molecular surface is opaque, so framing a buried ligand put the camera inside the surface and produced an unreadable interior view rather than a surface. `examples/surface.recipe.yaml` follows.
- `color: plddt` now renders with Mol\*'s AlphaFold `plddt-confidence` palette (orange = very low confidence, dark blue = very high). It mapped to the `uncertainty` theme, which colors high values red — AlphaFold stores pLDDT in the B-factor field, so every confidence render came out inverted, with folded domains red and disordered loops blue. The renderer now registers the model-archive quality-assessment behavior that provides the theme. `color: uncertainty` still selects the `uncertainty` theme.
- Selector values with embedded whitespace are rejected instead of silently compiled. The DSL has no boolean operators, so `chain:A and residue:5` parsed as `label_asym_id` `"A and residue"` — a selector that matches nothing, which `selectors explain` reported as valid.
- A blank render is now classified as a scene problem (`code: invalid_scene`, `agent_code: invalid_job`, not retryable) instead of a renderer failure. It previously reported `renderer_unavailable` with `retryable: true` and advised running `doctor`, so `diagnose` said "renderer unavailable" next to "renderer completed" and told agents to re-run an identical job that could never succeed.
- Bad command lines (unknown command, unknown flag, bad flag value) now report `code: invalid_input` / `agent_code: invalid_job` with exit 2, instead of `internal_error` with exit 1.
- `render --report FILE` now writes `run_id` and `run_log` into the report file, so the documented agent loop can drive `logs export` and `diagnose` from it. Previously those fields only reached stdout.
- `logs verify` and `logs rerun` now list every file in a `.molrun` bundle. Bundle reading stopped at `run.json`, so verify reported bundles this tool had just written as missing their `job.json` and `scene.mvsj` sidecars.
- `inspect` no longer rewrites author-supplied component and structure refs (lowercasing, `-` to `_`). The refs it reported did not exist in the compiled scene, so refs copied out of an inspect report failed when used for `camera.focus`.
- `inspect` selection stats now report `supported: false` with a `reason` when an input was never parsed, instead of `supported: true` with zero atoms. Any remote bcif input — the default — previously read as "your selector matched nothing".
- Diagnosis hints no longer suggest running `doctor` for unrelated failures. The catch-all matched bare `render`/`molstar` against the whole message, so any error carrying a path inside a molstar checkout was blamed on the renderer.

### Notes

- Linux amd64 is the primary renderer release target. macOS and Windows are contract-supported, while full renderer support depends on native Node canvas/GL dependencies.
