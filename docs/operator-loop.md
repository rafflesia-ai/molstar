# Golden Operator Loop

This is the recommended end-to-end loop for humans, agents, and CI systems that need a reproducible headless Mol* render workflow.

## 1. Check The Environment

Run these once per machine or container image:

```bash
molstar doctor --skip-probe --json
molstar agent doctor --json
molstar capabilities --json
```

Use `doctor` for runtime health, `agent doctor` for automation contracts, and `capabilities` when a workflow needs to branch on renderer support.

## 2. Validate Before Rendering

```bash
molstar job validate job.yaml --schema --json
molstar job explain job.yaml --json
molstar inspect job.yaml --semantic=auto --json
```

`validate` checks the schema, `explain` reports compile/render intent, and `inspect` gives semantic structure inventory when the renderer can load the scene.

## 3. Render With A Run Log

```bash
molstar render job.yaml \
  --run-label ligand-ci \
  --report artifacts/render-report.json \
  --mvs artifacts/scene.mvsj \
  --state artifacts/state.molj \
  --json
```

Run logs are enabled by default. Keep them enabled for CI and automation unless the job contains inputs that should never be embedded.

## 4. Replay Or Export The Run

```bash
molstar logs list --limit 10 --json
molstar logs show RUN_ID --json
molstar logs show RUN_ID --rerun --out-dir artifacts/rerun --json
molstar logs export RUN_ID --out artifacts/RUN_ID.molrun --json
molstar logs verify artifacts/RUN_ID.molrun --strict --json
molstar logs rerun artifacts/RUN_ID.molrun --out-dir artifacts/bundle-rerun --json
```

`logs verify` checks a portable `.molrun` bundle without importing it. Use `--strict` in CI so metadata-only bundles fail loudly when local inputs are missing.

## 5. Diagnose Failures

```bash
molstar render job.yaml \
  --ci-artifact artifacts/molstar-failure \
  --json

molstar diagnose RUN_ID --json
molstar diagnose --ci-artifact artifacts/molstar-failure --json
molstar diagnose RUN_ID \
  --ci-artifact artifacts/molstar-failure \
  --bundle \
  --out artifacts/molstar-issue.zip \
  --redact-paths \
  --redact-env \
  --redact-inputs \
  --json
```

`diagnose --bundle` is run-log centered: it requires a `RUN_ID` and can optionally fold in a CI artifact directory. The issue zip includes the diagnosis, run log, render report, job, scene when available, doctor/capability reports, optional CI files, and embedded local inputs within the configured size limits.

For a copy-paste CI upload pattern, see [CI Artifact Convention](ci-artifacts.md).

## 6. Smoke-Test Server Mode

```bash
molstar serve --socket /tmp/molstar.sock --job-store .molstar-jobs --quiet
molstar serve smoke --socket /tmp/molstar.sock --json
molstar serve smoke --socket /tmp/molstar.sock --render-probe --probe-out-dir artifacts/serve-smoke --json
molstar rpc capabilities --socket /tmp/molstar.sock --json
molstar server submit job.yaml --socket /tmp/molstar.sock --wait --download-outputs --out-dir artifacts/server --json
```

`serve smoke` checks health, readiness, capabilities, schema, metrics, and JSON-RPC capabilities against a running server. Add `--render-probe` for local deployments where the CLI and server share filesystem access; it submits a tiny render job and verifies output download before real work is sent.

## 7. Release Dogfood

```bash
npm run dogfood:molstar
npm run test:release:docker
```

The dogfood script exercises the loop above with local fixture data, portable run bundles, non-portable bundle verification, CI diagnosis, batch rendering, compatibility fixtures, benchmark dry runs, server smoke, JSON-RPC, and server submission. The container release verifier runs the release gate in a hermetic Linux image when workstation state is not trustworthy.
