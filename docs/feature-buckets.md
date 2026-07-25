# Headless feature buckets

## CLI Quickstart

Install dependencies and check the runtime:

```bash
npm install
go run ./cmd/molstar doctor --json
```

Create a friendly recipe, validate it strictly, and render it:

```bash
go run ./cmd/molstar recipe init ligand --id 1cbs --out ligand.recipe.yaml
go run ./cmd/molstar recipe validate ligand.recipe.yaml --schema --json
go run ./cmd/molstar render ligand.recipe.yaml --out ligand.png --json
```

Inspect the contracts before handing jobs to another process:

```bash
go run ./cmd/molstar recipe schema --out recipe.schema.json
go run ./cmd/molstar recipe explain ligand.recipe.yaml --schema --json
go run ./cmd/molstar job schema --out job.schema.json
go run ./cmd/molstar job explain ligand.recipe.yaml --json
```

Use the selector helpers when authoring recipes or render flags:

```bash
go run ./cmd/molstar selectors list
go run ./cmd/molstar selectors explain 'chain:A/residue:10-20' --json
```

Run the HTTP service with a bounded persistent queue:

```bash
go run ./cmd/molstar serve \
  --addr 127.0.0.1:8090 \
  --auth-token "$MOLSTAR_AUTH_TOKEN" \
  --job-store .molstar-jobs \
  --job-ttl 24h

curl -H "Authorization: Bearer $MOLSTAR_AUTH_TOKEN" http://127.0.0.1:8090/schema
go run ./cmd/molstar server submit ligand.recipe.yaml --url http://127.0.0.1:8090 --token "$MOLSTAR_AUTH_TOKEN" --json
go run ./cmd/molstar server status job_1 --url http://127.0.0.1:8090 --token "$MOLSTAR_AUTH_TOKEN" --json
go run ./cmd/molstar server wait job_1 --url http://127.0.0.1:8090 --token "$MOLSTAR_AUTH_TOKEN" --timeout 2m --download-outputs --out-dir outputs/server --json
go run ./cmd/molstar server logs job_1 --url http://127.0.0.1:8090 --token "$MOLSTAR_AUTH_TOKEN"
go run ./cmd/molstar server events job_1 --url http://127.0.0.1:8090 --token "$MOLSTAR_AUTH_TOKEN" --json
go run ./cmd/molstar jobs prune --job-store .molstar-jobs --ttl 24h --dry-run --json
```

For batch or CI usage, keep stdout machine-readable and send operational logs to stderr:

```bash
go run ./cmd/molstar fixtures verify --out-dir outputs/fixtures --golden --json
npm run assets:cli
npm run test:assets
go run ./cmd/molstar batch jobs.jsonl --concurrency 4 --continue-on-error --json
npm run bench:snapshot
npm run docker:verify
```

## Core

- `doctor` command for Node, npm, config path, primary/fallback renderer availability, validator availability, optional Docker daemon reachability, and network-free primary/fallback render probes unless `--skip-probe` is set; `doctor --fix` writes renderer config, creates a cache directory, installs missing npm renderer dependencies when needed, and reports Docker remediation guidance without hanging on a stuck daemon.
- `install-local` command for putting the Go CLI on `PATH` and writing a JSON runtime config that points installed commands back to this checkout's renderer assets.
- `examples`, `presets`, and `completion bash|zsh|fish|powershell` commands for first-run discovery, copy-pasteable workflows, and shell integration.
- CI coverage for Go tests, vet, TypeScript typechecking, schema generation, generated docs/completion drift, golden render probing, npm audit, package dry-run, cross-platform contract smoke, Linux renderer self-test/bench snapshots, Docker image build, and Docker smoke verification.
- `job schema`, `job validate --schema`, and `job explain` commands for a real draft 2020-12 JSON Schema contract, version policy reporting, strict unknown-field checks, and dry-run action reports that agents can inspect before rendering.
- `job init`, `job examples`, `job migrate`, and `job normalize` commands for creating, inspecting, migrating, and canonicalizing job specs.
- `recipe init`, `recipe schema`, `recipe validate --schema`, `recipe explain`, and `recipe compile` commands for friendly YAML recipes with a strict draft 2020-12 JSON Schema that compile to the canonical job/MVS path; `render`, `scene compile`, and `job normalize` accept recipe files directly.
- Stdin support with `-` for render, scene compile, scene validate, batch, and job normalize.
- JSON failure envelopes when `--json` is set, with classified exit codes for invalid input, validation, runtime blocks, render failures, doctor failures, and cancellation.
- Friendly render flags for identifier, URL, file, job, and `.mvsj/.mvsx` inputs.
- `molstar render --demo` for a tiny network-free local fixture render that exercises the full renderer path.
- Built-in presets through `presets list`, `presets show`, and `render --preset`, with predictable default output names such as `1cbs-ligand.png`.
- YAML/JSON job spec that compiles to MVSJ or bundled MVSX archives, including explicit user-provided archive assets.
- Selector DSL that keeps recipes/jobs readable while compiling to MVS selectors: `all`, `polymer`, `protein`, `nucleic`, `ligand`, `ion`, `water`, `chain:A`, `chain:A/residue:10-20`, `ligand:RET`, `atom:CA`, `element:C`, and `within:5A:ligand` via the Mol* surroundings extension. `selectors list` and `selectors explain` document the supported forms, while unsupported negation such as `not:water` fails with a targeted diagnostic.
- Common representations: `cartoon`, `ball-and-stick`, `spacefill`, `line`, `surface`, and `backbone`.
- Explicit colors as MVS color nodes; `element` compiles to explicit atom colors, while `chain`, `entity`, and `plddt` compile to Mol* theme hints consumed by the headless renderer. Render, scene, server, and batch reports include theme bridge diagnostics.
- Camera focus, standard views, explicit target/position camera, canvas background, image size, MVSJ output, and Mol* `.molj` state output.
- Owned Node renderer bridge (`scripts/render-mvs.js`) that loads MVSJ/MVSX through `HeadlessPluginContext`, with the official Mol* `mvs-render` CLI kept as a fallback path.
- JSON render and batch reports keep stdout machine-readable and suppress renderer progress by default; pass `--verbose` to show renderer progress or `--quiet` to suppress it explicitly.
- Renderer diagnostics through `render --diagnose`, including the resolved renderer command, runtime capabilities, and normal render command/output verification metadata in one JSON report.
- Local run history through `logs list`, `logs show`, `logs show --rerun`, `logs export`, and `logs import`, including explicit replay policy metadata, bounded embedded local inputs, `--include-inputs=false` metadata-only bundles, replay warnings, and top-level `diagnose` for run logs or `--ci-artifact` render failure directories.
- Transactional image/movie/MVSJ/MVSX/report writes with validation, including byte counts, dimensions, SHA-256, perceptual average hash, and non-blank checks in JSON reports.
- Batch rendering from JSONL/YAML/JSON jobs with concurrency, JSON reports, MVSJ/MVSX exports, output templating, retries, skip-existing verification, and manifest output.
- Runtime controls for profiles (`default`, `ci`, `locked`), cache, offline/no-network execution, host allowlists, local path allowlists, timeout, max pixels, max atoms for countable local/cached text structures, max outputs, max download bytes, and max archive bytes.
- Cache inspection commands: `cache list`, `cache explain`, `cache verify`, and `cache prune`.
- `inspect` command for pre-render job inspection, including planned outputs, runtime/cache actions, compile warnings/theme bridges, and local/cached text-structure counts/bounds for supported selectors.
- `compat check` command for a local compatibility corpus covering schema validation, MVS compilation, MVSX writing, inspection, and optional real render probing.
- `fixtures list` and `fixtures verify` commands for an acceptance corpus: local network-free rendering and MVSX packaging by default, deterministic metadata/hash checks behind `--golden`, and public ligand/surface/confidence cases behind `--network`.
- Long-lived HTTP mode through `serve` with public `/health`, optional bearer auth for all other endpoints, `/capabilities`, `/schema`, `/validate`, `/explain`, `/render`, `/rpc`, async `/jobs/{id}` status, cancellation, `/jobs/{id}/events`, persistent Node renderer workers, startup `--prewarm`, subprocess fallback, bounded workers, bounded queueing, request timeouts, Unix socket listening, queue metrics, optional `--job-store` persistence, startup `--job-ttl` pruning, and `jobs prune` cleanup.
- `server submit`, `server status`, `server wait`, `server logs`, `server events`, and `server cancel` client commands for talking to a running `serve` instance over HTTP or a Unix socket without hand-written curl; `server wait --download-outputs` downloads verified output files via `/jobs/{id}/outputs/{index}`.
- TypeScript scene builder API that emits the same job spec, accepts typed selector/color/representation helpers, shells through the CLI for rendering/export, returns parsed reports, and cleans temporary job files.
- Thin Python subprocess wrapper that accepts the same job spec, shells through the installed CLI, parses JSON reports, raises structured errors, and stays independent of Mol* internals.
- Benchmark snapshots through `bench --report`, `bench --baseline`, and `npm run bench:snapshot`, including CLI provenance, platform data, measured runs, percentiles, and optional regression thresholds.
- Generated CLI docs and shell completions through `npm run assets:cli`; local/release packaging regenerates and verifies these assets rather than relying on stale folders.
- Docker packaging that bundles the Go CLI, the dedicated Node renderer entrypoint, Mol* dependencies, schemas, docs, completions, examples, and the Python wrapper; `scripts/verify-docker.sh` fails fast when the Docker daemon is unreachable and smoke-tests doctor repair, recipe, golden fixtures, selectors, wrapper import, benchmark, server wait/logs/downloads, RPC, and rendering inside the image.
- Release packaging supports `.tar.gz`, `.zip`, and unpacked artifact installs, validates archive contents before smoke tests, rejects macOS metadata files, ships Darwin zip archives for notarization, and includes `scripts/notarize-macos-archives.sh` for credentialed signing/notary submission.

## Exhaustive

- Full MVS schema coverage: annotations, component-from-source/URI, color-from-source/URI, volumes, primitives, opacity, clipping, transforms, instances, and multi-state stories.
- Advanced renderer control: transparency, antialiasing, postprocessing, outlines, ambient occlusion, renderer capability snapshots, and render pass diagnostics.
- Authenticated multi-tenant operation and database-backed external job storage.
- Deeper sandboxing for renderer subprocesses, archive quotas, URI policies per endpoint, and reproducible renderer capability profiles.
- Analysis outputs for selections, bounds, contacts, labels, picking, and rendered object inventories.
- Plugin extension loading, custom representations, custom property providers, and raw Mol* `PluginContext` escape hatches.
- Language wrappers that submit the same job spec instead of binding directly to Mol* internals.
