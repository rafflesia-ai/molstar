# Headless Cookbook

Short recipes for running Mol* without a browser.

## Render A Local Structure

```bash
molstar render model.cif --format mmcif --out render.png --size 1200x900 --json
```

Use `--dry-run --json` first when an agent needs to validate paths, outputs, and renderer commands without creating files.

## Render A Public PDB Entry

```bash
molstar render 1cbs \
  --preset ligand \
  --focus ligand \
  --size 1200x900 \
  --cache .molstar-cache \
  --out outputs/1cbs-ligand.png \
  --json
```

## Compile A Reproducible Scene

```bash
molstar recipe validate examples/ligand.recipe.yaml --schema --json
molstar recipe explain examples/ligand.recipe.yaml --schema --json
molstar recipe compile examples/ligand.recipe.yaml --out outputs/ligand.mvsj --json
molstar render outputs/ligand.mvsj --out outputs/ligand.png --size 1200x900 --json
```

## Offline Cache

Prime the cache once:

```bash
molstar render 1cbs --cache .molstar-cache --out outputs/prime.png --json
```

Then require offline inputs:

```bash
molstar render 1cbs --cache .molstar-cache --offline --out outputs/offline.png --json
```

## Batch Render

```bash
molstar batch jobs.jsonl --concurrency 4 --continue-on-error --json
```

Use one JSON job per line. Keep progress and diagnostics on stderr and parse the final JSON report from stdout.

## Operational Loop

For the canonical render, replay, diagnose, and server-smoke workflow, see [Golden Operator Loop](operator-loop.md).

Render with a label so later logs are easy to scan:

```bash
molstar render --demo \
  --out outputs/demo.png \
  --size 800x600 \
  --run-label ci-smoke \
  --json
```

Inspect recent runs:

```bash
molstar logs list --limit 10
molstar logs --last --json
molstar logs show RUN_ID --json
```

Rerun the normalized job from a saved log without re-authoring the original command:

```bash
molstar logs show RUN_ID --rerun --out-dir outputs/rerun --json
```

Export a portable run bundle for another machine or a CI artifact:

```bash
molstar logs export RUN_ID --out artifacts/run.molrun --json
molstar logs verify artifacts/run.molrun --strict --json
molstar logs rerun artifacts/run.molrun --out-dir outputs/rerun --json
molstar logs import artifacts/run.molrun --dir .molstar-runs --json
```

Local input bytes are embedded by default with bounded limits: each input is capped by `--max-single-input-bytes` and the total is capped by `--max-input-bytes`. To export only the report/job metadata, use:

```bash
molstar logs export RUN_ID \
  --include-inputs=false \
  --out artifacts/run-metadata-only.molrun \
  --json
```

The JSON `replay` block reports whether the bundle is replayable, fully replayable, which inputs were embedded, which remote inputs will be fetched again, and which local inputs are missing. Non-portable bundles keep the warnings in the imported run log.

For failures in CI, ask `render` to write a diagnostic directory:

```bash
molstar render job.yaml \
  --out outputs/render.png \
  --ci-artifact artifacts/molstar-render-failure \
  --json
```

Diagnose either a run id or a CI artifact:

```bash
molstar diagnose RUN_ID --json
molstar diagnose --ci-artifact artifacts/molstar-render-failure --json
molstar diagnose RUN_ID --ci-artifact artifacts/molstar-render-failure --bundle --out artifacts/issue.zip --redact-paths --redact-env --redact-inputs --json
```

`diagnose` summarizes the likely cause, renderer status, replayability, warnings, and the next command to run. `diagnose --bundle` writes a run-log-centered issue zip; it requires a run id and can also include CI artifact files.

## Inspect For Agents

```bash
molstar inspect job.yaml --semantic=auto --json
```

`--semantic=auto` reports exact Mol* structure/representation metadata when WebGL is available and returns a structured semantic skip/error when it is not. Use `--semantic=false` for compile-only inspection and `--strict-semantic` when semantic inspection must be mandatory.

## Serve Locally

```bash
molstar serve --addr 127.0.0.1:8080 --workers 2 --queue 32 --job-store .molstar-jobs
```

Submit and wait:

```bash
molstar server submit job.yaml --url http://127.0.0.1:8080 --wait --download-outputs --out-dir outputs --json
```

Smoke-test a running server before sending work:

```bash
molstar serve smoke --url http://127.0.0.1:8080 --json
molstar serve smoke --url http://127.0.0.1:8080 --render-probe --probe-out-dir outputs/serve-smoke --json
```

Metrics:

```bash
curl -sS http://127.0.0.1:8080/metrics
curl -sS http://127.0.0.1:8080/metrics/prometheus
```

OpenAPI:

```bash
molstar serve --openapi > openapi.json
```

The OpenAPI document includes render and JSON-RPC client snippets under `x-codeSamples`.

## Docker Runtime And Render Smoke

Build once:

```bash
npm run docker:build
```

Run deterministic runtime checks that do not require rendering:

```bash
npm run docker:verify:runtime
```

Run strict renderer checks, including visual hash assertions:

```bash
npm run docker:verify:render
```

Run packaged-artifact checks inside Docker on a Linux artifact:

```bash
npm run package:local
npm run docker:verify:artifact
```

## Benchmark Regressions

Capture a baseline:

```bash
molstar bench job.yaml --iterations 5 --warmup 1 --report outputs/benchmarks/baseline.json --json
```

Fail when avg or p95 regresses beyond a threshold:

```bash
molstar bench job.yaml --baseline outputs/benchmarks/baseline.json --fail-regression 20% --json
```

## Error Handling

All JSON error envelopes include:

- `error.code`: internal CLI/server classifier.
- `error.agent_code`: stable agent-oriented classifier such as `invalid_job`, `webgl_unavailable`, `renderer_unavailable`, `network_blocked`, `security_policy`, or `server_busy`.
- `error.retryable`: whether a retry may be useful.
- `error.exit_code`: CLI exit code for subprocess callers.
