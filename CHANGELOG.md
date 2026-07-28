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

- A job whose only outputs are exports (`mvsj`, `mvsx`, `molj`) now succeeds. It wrote the artifact correctly and then failed with `no image or video outputs configured`, so a pipeline gating on the exit code discarded a good bundle. A job that produces nothing at all is still rejected.
- `serve` explains why it could not listen. A `--socket` path over the ~104-byte `sun_path` limit — easy to hit under a CI temp directory — failed as a bare `bind: invalid argument`, and a taken port as `bind: address already in use`, with no suggested remedy.
- `outputs[].transparent` actually renders a transparent background. The option is in the job schema, the Python client, and the OpenAPI schema, but was never passed to the renderer, so it silently produced a fully opaque image. Transparency is set on the image pass as well as the Canvas3D — the saved PNG comes from the image pass, so setting it on the canvas alone leaves the background opaque — and the persistent worker's plugin cache key now includes it.
- `scene.canvas.background: transparent` is rejected during validation with a pointer to `transparent: true` on the output. Mol\* rejects it as a color, so the job compiled and then failed inside the renderer as a retryable `renderer_unavailable`, hiding a plain authoring mistake. Malformed hex colors are rejected too.
- Concurrent downloads of the same input no longer fail. The download temp file was a fixed `<cachePath>.tmp` derived from the URL, so every concurrent fetch of one input shared a single file: they overwrote each other and all but one rename failed with `ENOENT`. `molstar batch --concurrency 8` failed 4 of 8 jobs whenever they shared an uncached input, and so did separate processes sharing a cache directory. Cache sidecars are written atomically for the same reason.
- A cache entry whose size no longer matches what was recorded is detected before rendering. It was handed to the renderer, which failed with an opaque `renderer_unavailable` marked retryable, so an offline caller retried a broken file forever. It is now re-downloaded when network access is available, and reported as a non-retryable `invalid_input` naming `molstar cache prune` when it is not.
- Network transport failures classify as `network_error`/`network_blocked` and are retryable. A DNS or connection failure during runtime preparation was typed as a runtime block before its message was consulted, so it reported `security_policy` with `retryable: false` — telling callers a policy had blocked a fetch that had simply found the network down. A deliberately disabled network with an empty cache still reports non-retryable, because retrying it can never help.
- `batch --json` keeps its JSON Lines stream line-delimited. When a job failed, a pretty-printed multi-line error envelope was appended to the same stream, so line-by-line consumers hit a bare `{` and failed to parse. The aggregate failure is also classified from the constituent job failures instead of being hardcoded to `render_failed`/`renderer_unavailable`/retryable.
- `batch --retries` no longer retries jobs whose failure is not retryable, such as a bad selector or a policy rejection.
- Duplicate `ref` values in a scene are rejected. MVS refs address nodes in the compiled document, so duplicates made `camera.focus` bind to whichever came first and collapsed the renderer's ref map, silently dropping one component's semantic stats.
- Local inputs under a path containing a space, a non-ASCII character, or a parenthesis now render. Those paths are handed to the renderer as correctly percent-escaped RFC 8089 `file://` URLs, but Mol\*'s Node file reader strips the scheme without decoding, so an ordinary path such as `~/Desktop/my project/model.pdb` failed with `ENOENT` and surfaced as `renderer_unavailable`.
- `runtime.max_atoms` reports when it could not be enforced. Atom counts are unavailable for BinaryCIF and trajectory formats, and BinaryCIF is what every `pdbe`/`rcsb` identifier resolves to, so the limit silently did not bind on the most common input path — including under the `locked` and `ci` profiles that set it.
- `render --explain`, `batch`, and `inspect` no longer discard runtime warnings. Each replaced the accumulated warning list with the scene-compilation warnings instead of extending it, so anything reported during runtime preparation was dropped from the report.
- Every HTTP server error response now carries the standard `{ok, command, error{code, agent_code, message, retryable, exit_code, diagnosis}, timestamp}` envelope. Job/output 404s and 405s answered with a bare `{"ok":false,"error":"job not found"}` string, and unmatched paths fell through to net/http's plain-text `404 page not found`, so `error.agent_code` was missing exactly when a request had failed.
- The `serve --openapi` error schema documents `agent_code`, `retryable`, and `exit_code`. It listed only `code`, `message`, and `diagnosis`, omitting the classifier `docs/json-contracts.md` tells agents to branch on.
- All retention flags accept the same duration syntax. `logs prune --older-than` accepted days (`14d`) while `cache prune --older-than`, `jobs prune --ttl`, and `serve --job-ttl` rejected them with `time: unknown unit "d"`; they now share one parser and one error message.
- A renderer capability probe killed by its deadline reports the timeout and suggests `--timeout` instead of the bare `signal: killed`.
- `--timeout` and server-side job cancellation interrupt renderer commands that spawn a child process. The renderer ran under `exec.CommandContext`, which signals only the direct child; a grandchild kept the inherited stdout/stderr pipes open, so the wait never returned and the render hung indefinitely instead of timing out. The renderer now runs in its own process group and the whole group is killed on cancellation, so the grandchild does not leak either.
- `GET /jobs/{id}/events` streams a running job to its terminal event. It wrote a one-shot snapshot of the events recorded so far, so the documented async flow — submit, then stream — closed the stream mid-render and never delivered `succeeded`/`failed`.
- `inspect --semantic` works on structures whose inspect payload exceeds 16 KiB. It parsed the report-truncated copy of the renderer's stdout, so anything larger than the truncation limit — 4HHB's payload is ~24 KB — failed to decode and returned no semantic stats at all. Reports still carry the truncated copy.
- `inspect` records a top-level warning when semantic inspection fails in non-strict mode. The failure was only visible inside the nested `semantic` object, so `ok` stayed true and `warnings` stayed empty while the renderer-computed stats were silently missing.
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
