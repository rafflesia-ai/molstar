# JSON Contracts

The CLI has two JSON stability levels.

## Command Matrix

| Surface | Success stdout shape | Success marker | Failure stdout shape with `--json` | Stable branch fields |
| --- | --- | --- | --- | --- |
| `job validate --json` | JSON report | `ok: true` | error envelope | `ok`, `file`, `schema` |
| `job explain --json` | JSON report | `ok: true` | error envelope | `schema`, `would_compile`, `would_render`, `inputs`, `outputs` |
| `job normalize --json` | normalized job JSON, unless `--write` sends it to a file | valid job `version` | error envelope | `version`, `inputs`, `scene`, `outputs` |
| `render --json` | render report | `ok: true` | error envelope | `ok`, `output_files`, `commands`, `diagnostics`, `run_id`, `run_log`, `job`, `mvs_document` |
| `batch --json` | JSON Lines, one report per job | each line has `ok` | JSON Lines with failed job reports, or error envelope for command setup failures | per-line `ok`, `input`, `output_files`, `attempts`, `error` |
| `logs list --json` | JSON report | `ok: true` | error envelope | `runs[*].id`, `runs[*].ok`, `runs[*].replayable`, `runs[*].fully_replayable` |
| `logs show --json` | JSON report | `ok: true` | error envelope | `run.id`, `run.report`, `run.replay` |
| `logs verify --json` | JSON report | `ok: true` | error envelope; with `--strict`, non-replayable bundles also exit non-zero after writing the report | `bundle`, `id`, `replayable`, `fully_replayable`, `replay`, `expected_outputs` |
| `logs rerun --json` | JSON report | `ok: true` | error envelope | `bundle`, `run.id`, `rerun.ok`, `rerun.output_files` |
| `diagnose RUN_ID --json` | diagnosis report | `ok: true` | error envelope | `failed`, `likely_cause`, `renderer_status`, `replayable`, `next_command` |
| `diagnose --ci-artifact DIR --json` | diagnosis report | `ok: true` | error envelope | `source`, `failed`, `error`, `artifact_files`, `next_command` |
| `diagnose RUN_ID --bundle --json` | bundle report | `ok: true` | error envelope | `output`, `run_id`, `files`, `replay`, `redactions` |
| `serve smoke --json` | smoke report | `ok: true` | smoke report then non-zero when checks fail | `checks[*].name`, `checks[*].ok`, `checks[*].status`, optional `checks[*].job_id` |
| `server * --json` | server REST response or wrapped wait report | `ok: true` on server body | error envelope for client/setup failures; server error body for HTTP failures | job `id`, `status`, `report`, `error`, `downloaded_outputs` |
| `rpc * --json` | JSON-RPC envelope | `result.ok: true` for built-in methods | JSON-RPC error envelope, then non-zero | `jsonrpc`, `result`, `error.code`, `error.message` |
| `serve --openapi` | OpenAPI document | `openapi` field | error envelope | `openapi`, `paths`, `components`, `x-codeSamples` |

Commands that print artifacts rather than reports, such as `job schema --out file` or `scene compile --out file`, keep stdout quiet when writing to a file unless they explicitly document a JSON report.

`render --report FILE` writes the same report to `FILE` that `--json` writes to stdout, including
`run_id` and `run_log`, so an agent can drive `logs export`, `logs verify`, and `diagnose` straight
from the report file.

## Stable For Agents

Agents may branch on these fields:

- `ok`: boolean success marker on JSON reports.
- `error.code`: internal command/server classifier.
- `error.agent_code`: stable automation classifier.
- `error.retryable`: whether retrying may help.
- `error.exit_code`: expected CLI process exit code.
- `error.diagnosis`: zero or more remediation hints.
- `capabilities.matrix[*]`: renderer target availability and high-level GL/canvas status.
- `render --dry-run --json`: `commands[*].skipped`, `outputs`, `output_files`, `diagnostics.renderer_mode`, `job`, and `mvs_document`.
- `job validate --schema --json`: `ok`, `file`, and `schema`.
- `job explain --json`: `ok`, `schema`, `would_compile`, `would_render`, `inputs`, and `outputs`.
- `serve --openapi`: OpenAPI version, paths, schemas, examples, and `x-codeSamples`.
- `/metrics` and `/metrics/prometheus`: submitted/succeeded/failed counters and queue/render histogram families.
- `agent doctor --json`: `contract`, `steps[*].name`, `steps[*].ok`, `steps[*].agent_code`, and `advice`.
- `logs --last --json` and `logs show --json`: `run.replay.replayable`, `run.replay.fully_replayable`, `run.replay.embedded_inputs`, `run.replay.missing_inputs`, and `run.replay.warnings`.
- `logs verify --json`: `replayable`, `fully_replayable`, `warnings`, and `expected_outputs`.
- `logs rerun --json`: bundle path, run id, and rerun render report.
- `diagnose --bundle --json`: output zip path, included file list, and redaction flags.
- `serve smoke --json`: aggregate `ok`, per-check status, and render-probe job/output counts when `--render-probe` is used.

## Diagnostic Fields

Treat these as useful but not stable for branching:

- absolute paths, temporary paths, command paths, and cache paths;
- timestamps and durations;
- renderer stdout/stderr snippets;
- platform, Node, Mol*, GL, and canvas implementation details;
- ordering of warning strings when several warnings apply.

## Error Policy

Agents should branch on `error.agent_code`, not message text.

The full set of values:

- `invalid_job`: fix flags, schema, selectors, paths, outputs, or scene data before retrying. Also
  covers a scene that rendered nothing visible (`code: invalid_scene`) and bad command lines such as
  an unknown flag or command (`code: invalid_input`).
- `webgl_unavailable`: renderer exists but cannot create headless WebGL.
- `renderer_unavailable`: renderer command failed, the fallback also failed, native modules were
  built for the wrong Node ABI, or the renderer loaded the scene but export failed.
- `network_blocked`: cache/offline/network policy prevented a fetch.
- `security_policy`: allow-path, archive, runtime, or sandbox policy blocked work.
- `server_busy`: retry later or reduce concurrency.
- `canceled`: caller or server canceled the job, or a job timeout elapsed.
- `internal_error`: unclassified; capture the whole envelope and report it.

The `agent_code` to `code` rollup, expected exit codes, and retryability are tabulated in the
[README](../README.md) and pinned by `TestErrorCodeMatrix` in `internal/cli`.

A blank render reports `code: invalid_scene` / `agent_code: invalid_job` and is **not** retryable:
the renderer worked and the scene had nothing visible in it, so rerunning an identical job cannot
help. Use `molstar inspect JOB --semantic=auto --json` to see what the selectors actually matched.

When `retryable` is false, retry only after changing the job or environment. When `retryable` is true, retry with backoff and include the original JSON envelope in logs.

## Snapshot Tests

Stable CLI/server JSON shapes are frozen under:

```text
internal/cli/testdata/contracts/
```

Run:

```bash
npm run test:contracts
```

For intentional Mol* CLI contract changes:

```bash
UPDATE_CONTRACT_SNAPSHOTS=1 npm run test:contracts
```

Snapshot updates should be reviewed with the compact assertions in mind: if a stable field changes, update this document and the agent workflow docs in the same change.

Snapshot `shape` sections intentionally normalize volatile values. Timestamps, durations, local paths, command vector lengths, and environment-dependent booleans are not stable branch targets; the tests project those to placeholders and keep real assertions in the `stable` section.

## Run Log Privacy

Run logs can embed local input bytes so `logs show --rerun` can replay temporary or later-deleted inputs. Control this with:

```bash
molstar render job.yaml --log-assets=false
molstar render job.yaml --log-asset-max-bytes 1048576 --log-assets-max-bytes 8388608
```

When local inputs are not embedded, `run.replay.fully_replayable` is false and `run.replay.warnings` explains why.

`diagnose --bundle` is intentionally anchored on a run log. It requires `RUN_ID`, writes a zip issue bundle, and may include `--ci-artifact DIR` to copy CI-side reports into the same bundle. CI-artifact-only diagnosis remains `molstar diagnose --ci-artifact DIR --json`.

Issue bundles include `redactions.json`. The `--redact-paths`, `--redact-env`, and `--redact-inputs` flags are stable and reflected in the top-level JSON report.
