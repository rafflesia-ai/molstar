# Agent Workflows

This CLI is designed so agents can use the high-level commands first and only drop to raw MVS or Mol* state when needed.

## Default Loop

1. Run `molstar agent doctor --json` once per environment.
2. Use `molstar job validate JOB --schema --json` and `molstar job explain JOB --json` before expensive renders.
3. Use `molstar inspect JOB --semantic=auto --json` when the agent needs structure counts, refs, camera state, or representation inventory.
4. Render with `molstar render JOB --json --report render-report.json`.
5. Export, verify, and rerun portable run bundles with `molstar logs export RUN_ID --out run.molrun --json`, `molstar logs verify run.molrun --strict --json`, and `molstar logs rerun run.molrun --out-dir rerun --json`.
6. Diagnose failures with `molstar diagnose RUN_ID --json`, and write an issue bundle with `molstar diagnose RUN_ID --bundle --out issue.zip --redact-paths --redact-env --redact-inputs --json` when handing work to another environment.
7. Treat stdout as the machine-readable result and stderr as progress or diagnostics.

For CI or offline tasks, set `runtime.cache`, `runtime.offline`, `runtime.allow_paths`, and `runtime.allow_hosts` in the job spec. Prefer explicit cache directories over implicit network fetches.

The stable JSON field policy is in `docs/json-contracts.md`. The complete golden operator loop is in `docs/operator-loop.md`.

## Error Handling

Every JSON command reports an `error.code` and `error.agent_code` when it fails. Agents should branch on `error.agent_code`, not on message text.

- `invalid_input`: fix flags, file paths, or job JSON shape.
- `validation_failed`: schema validation failed.
- `invalid_scene`: the job compiled to an invalid MVS scene.
- `runtime_blocked`: runtime policy, cache, path, timeout, or resource limits blocked execution.
- `security_policy`: allowlist, offline, path, host, or auth policy rejected the request.
- `network_error`: fetch failed or offline cache was missing.
- `renderer_unavailable`: Node, Mol*, GL, canvas, or renderer command is missing.
- `renderer_abi_mismatch`: native renderer modules were built for the wrong Node ABI or platform.
- `server_busy`: server queue or worker capacity is full; retry with backoff or use async submission.
- `render_failed`: Mol* loaded the scene but rendering/export failed.
- `canceled`: the request context or async job was canceled.

## Server Mode

Use `molstar serve --prewarm --auth-token "$TOKEN"` for long-running workers. Agents can discover the HTTP contract with:

```sh
molstar serve --openapi
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/schema/openapi
```

Operational endpoints:

- `GET /health`: public process health, queue state, readiness, and current metrics snapshot.
- `GET /ready`: public readiness status; returns `503` until required prewarm succeeds.
- `GET /metrics`: JSON counters for queue wait, render duration, failures by error code, worker fallbacks, and worker restarts.
- `POST /render?async=true`: submit and poll `/jobs/{id}`.
- `GET /jobs/{id}/events`: JSON Lines event stream for an async job.
- `POST /rpc`: JSON-RPC methods `capabilities`, `metrics`, `validate`, `explain`, and `render`.

Use async submission for batches. Retry only `server_busy`, transient `network_error`, and canceled requests that the caller intentionally timed out.

Before submitting work to a long-lived server, run:

```sh
molstar serve smoke --url http://127.0.0.1:8080 --json
```

For Unix-socket deployments, use `--socket /path/to/molstar.sock`.
For local deployments where the agent and server share filesystem access, add `--render-probe --probe-out-dir artifacts/serve-smoke` before submitting work.

## Benchmarking

Run a local no-network smoke benchmark:

```sh
molstar bench --json
```

Benchmark a real job with persistent workers:

```sh
molstar bench job.yaml --renderer-mode worker --iterations 10 --warmup 2 --json
```

The report includes measured runs only in the summary; warmup runs remain in `runs` with `warmup: true`.

## Artifact Policy

Agents should write artifacts to task-specific directories and keep the render report next to images:

```sh
molstar render job.yaml \
  --json \
  --report artifacts/render-report.json \
  --out artifacts/render.png
```

For reproducibility, also emit MVS and Mol* state when useful:

```yaml
outputs:
  - type: image
    path: artifacts/render.png
    size: [1600, 1200]
  - type: mvsj
    path: artifacts/scene.mvsj
  - type: molj
    path: artifacts/state.molj
```

Use `molstar inspect artifacts/scene.mvsj --semantic --json` to verify the loaded scene after compilation.
