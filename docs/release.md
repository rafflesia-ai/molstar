# Release and platform confidence

This project has three release gates:

1. Generated contracts are current.
2. Packaged artifacts install and run outside the checkout.
3. Renderer behavior is checked on Linux, with lighter contract smoke on Linux, macOS, and Windows.

## Local release check

Run the full local gate before cutting a tag:

```bash
npm run test:release
```

That command runs Go tests, `go vet`, TypeScript typechecking, renderer JavaScript syntax checking, generated schema diffs, generated CLI docs/completion diffs, local builds, `doctor`, GoReleaser config validation when available, local package creation, and installed-artifact smoke tests for `molstar`.

`package:local` writes per-step logs and `package-report.json` under `dist/package-logs` by default. Long-running build, docs, completion, dependency, and archive steps are timeout-bounded so a release failure leaves a concrete log path instead of a stuck terminal.

When another local cleanup job owns `dist`, isolate the release verifier:

```bash
VERIFY_RELEASE_DIST_DIR=/Volumes/ExtremeSSD/headlessmolstar/verify-dist \
GOCACHE=/Volumes/ExtremeSSD/headlessmolstar/go-build-cache-verify \
GOMODCACHE=/Volumes/ExtremeSSD/headlessmolstar/go-mod-cache-verify \
npm run test:release -- --skip-docker
```

When local executable launch behavior or renderer libraries are not reproducible, run the same release gate in the hermetic Linux dev-test image:

```bash
npm run test:release:docker
```

That command builds `Dockerfile.dev-test`, runs `scripts/verify-release.sh` inside it, and writes reports under `dist/verify-release-docker/`. Add runtime Docker render smoke with:

```bash
npm run test:release:docker:render
```

Use Docker verification when the daemon is available:

```bash
npm run docker:build
npm run docker:verify:runtime
npm run dogfood:molstar:docker
```

The runtime Docker profile is deterministic and does not require a working headless WebGL renderer. Use the strict render profile before publishing images intended for rendering:

```bash
npm run docker:verify:render
npm run dogfood:molstar:docker:render
```

Both profiles write `docker-smoke-summary.json` when `DOCKER_VERIFY_OUT_DIR` is set, which CI uploads as a smoke artifact.

For a clean Linux-style verification from a local checkout, run:

```bash
npm run test:linux-clean
```

That wrapper builds the Linux dev-test image from scratch, runs the release verifier in the image, builds default and full runtime images, verifies both runtime profiles, dogfoods the real MDAnalysisTests AdK fixture, and verifies the exported Linux package artifact when one is produced. It writes a machine-readable report under `dist/linux-clean`.

Useful local overrides:

```bash
VERIFY_LINUX_CLEAN_NO_CACHE=0 npm run test:linux-clean
VERIFY_LINUX_CLEAN_RELEASE_GO_PARALLELISM=2 npm run test:linux-clean
VERIFY_LINUX_CLEAN_RELEASE_GO_CACHE=1 npm run test:linux-clean
```

The Dockerized release runner also honors `VERIFY_RELEASE_DOCKER_RUN_RETRIES`, `VERIFY_RELEASE_DOCKER_GO_CACHE`, and `VERIFY_RELEASE_DOCKER_GO_PARALLELISM` for flaky local Docker daemons or constrained runners.

On GPU runners, use:

```bash
npm run dogfood:molstar:docker:gpu
```

Verify a packaged Linux artifact inside the Docker image:

```bash
npm run package:local
npm run docker:verify:artifact
```

## Release candidate artifact

Create a local release candidate:

```bash
npm run release:candidate
```

The command runs the release gate, copies the packaged archive into `dist/release-candidates/<label>/`, writes a `.sha256` file, and emits `manifest.json` with the git commit, dirty-worktree flag, platform, artifact digest, artifact-verification status, and Docker check statuses.

On Linux CI, include Docker smoke:

```bash
npm run release:candidate -- --docker
```

For the strict visual renderer gate:

```bash
npm run release:candidate -- --docker --render-docker
```

Docker packaged-artifact smoke requires a Linux artifact. On macOS or Windows, create the local RC without Docker and rely on the Linux release-candidate workflow for Docker verification.

When `npm run test:release` has already produced and verified an artifact, create only the RC manifest/copy layer with:

```bash
npm run release:candidate -- --reuse-verified-artifact
```

Pass `--artifact path/to/archive.tar.gz` when the artifact is not the newest `dist/headlessmolstar-local-*` archive.

## Generated assets

CLI reference docs, completions, and schemas are generated assets. Refresh them with:

```bash
npm run assets:cli
npm run schema
```

Verify they are committed and current with:

```bash
npm run test:assets
```

Verify agent-facing JSON contracts with:

```bash
npm run test:contracts
```

The stable/diagnostic field policy is documented in `docs/json-contracts.md`.

For render failure artifact upload conventions, see `docs/ci-artifacts.md`.

## Dependency audit

Run the dependency, vulnerability, and license inventory gate with:

```bash
npm run audit:deps
```

Set `AUDIT_OUT=dist/audit npm run audit:deps` to persist `npm-audit.json`, `govulncheck.jsonl` when `govulncheck` is installed, Go module inventory, npm dependency tree, and a license summary. The release gate writes this report to `dist/audit`.

## Package smoke

Create and verify a local release-like runtime:

```bash
npm run package:local
npm run test:artifact
```

The artifact verifier runs through:

```bash
npm run test:artifact:molstar
```

`test:artifact:molstar` installs the archive through `molstar install-artifact`, runs `molstar self-test`, and exercises inspect/compat/RPC/server flows.

`install-artifact` accepts unpacked runtimes, `.tar.gz`, and `.zip` archives.

## Installed Mol* Dogfood

For the full installed Mol* workflow, run:

```bash
npm run dogfood:molstar
```

This builds or reuses `bin/molstar`, installs it into a temporary bin directory, validates local recipes/jobs/scenes, renders fixture and demo images, exports and verifies `.molrun` bundles, writes a diagnostic issue zip, exercises CI-artifact diagnosis, batch rendering, compatibility checks, fixture verification, benchmark dry runs, server smoke, JSON-RPC, and server submission. `scripts/verify-release.sh` runs this same dogfood path after building the release binary. Use `DOGFOOD_KEEP=1` to keep artifacts for inspection.

## Review grouping

For a large dirty worktree, generate a review split with:

```bash
npm run review:groups
REVIEW_GROUPS_FORMAT=json npm run review:groups
```

Use the output to split review into release candidate, JSON contracts, run logs, Docker/CI, generated assets, dependency audit, and miscellaneous changes.

For a machine-readable split with staging commands:

```bash
npm run review:groups -- --json
```

## Benchmark snapshots

Capture a repeatable benchmark snapshot:

```bash
npm run bench:snapshot
```

Useful environment overrides:

```bash
ITERATIONS=5 WARMUP=1 SIZE=512x512 RENDERER_MODE=worker LABEL=release-candidate npm run bench:snapshot
BASELINE=outputs/benchmarks/previous/bench.json MAX_REGRESSION_PERCENT=20 npm run bench:snapshot
molstar bench job.yaml --baseline outputs/benchmarks/previous/bench.json --fail-regression 20% --json
```

The benchmark report includes CLI provenance, platform data, measured runs, summary percentiles, and optional baseline comparison.

## CI shape

The main CI job runs the full Ubuntu path with native renderer dependencies, real rendering, package smoke, Docker build, and Docker smoke.

The `platform-smoke` matrix runs on `ubuntu-latest`, `macos-latest`, and `windows-latest`. It builds the Go CLI, checks schema/recipe/dry-run rendering/docs/completions, and runs the Python wrapper tests without requiring native GL.

The `linux-renderer` job captures CPU renderer capability, self-test, and benchmark reports. The `linux-gpu-renderer` job is gated by the repository variable `RUN_HEADLESSMOLSTAR_GPU_CI=1` and expects a self-hosted runner labeled `self-hosted`, `linux`, and `gpu`.

The `release candidate` workflow is manually triggered from GitHub Actions. It runs `scripts/release-candidate.sh --docker`, uploads the release-candidate manifest/log/artifact copy, and can add `--render-docker` when the workflow input `run_render_docker` is set to `true`.

For operator-level usage recipes, see `docs/headless-cookbook.md` and the canonical loop in `docs/operator-loop.md`.

For platform support expectations, see `docs/platform-support.md`. Release notes are tracked in `CHANGELOG.md`.

## macOS signing and notarization

Darwin GoReleaser archives are zip files so they can be signed and submitted to Apple notarization. On macOS, after release archives exist under `dist/`, run:

```bash
APPLE_DEVELOPER_IDENTITY="Developer ID Application: Example (TEAMID)" \
APPLE_NOTARY_PROFILE=headlessmolstar \
./scripts/notarize-macos-archives.sh dist/*_darwin_*.zip
```

Alternatively set `APPLE_ID`, `APPLE_TEAM_ID`, and `APPLE_APP_SPECIFIC_PASSWORD` instead of `APPLE_NOTARY_PROFILE`.

The script signs `molstar` inside each zip with the hardened runtime option, rebuilds the archive, submits it with `xcrun notarytool`, and writes a `.notary.log` next to the archive.
