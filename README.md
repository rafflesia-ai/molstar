# molstar — headless Mol\*, built for agents

[Mol\*](https://molstar.org) is the molecular viewer that runs in your browser. This is a CLI that
drives it **without one**: no browser, no display, no human clicking a camera into place. You describe
a scene in YAML or JSON, and you get back a PNG, an `.mvsj` scene, and a machine-readable report.

It exists because coding agents are increasingly the ones asking for molecular images — to check a
docking pose, to illustrate a variant, to render a figure inside a pipeline — and a GUI is exactly the
wrong interface for that. Every design decision here follows from that: deterministic output, stable
error codes, replayable runs, and a diagnostic loop the agent can run on itself before asking a human.

```bash
molstar render 1cbs --preset ligand --out 1cbs.png --size 1200x900 --json
```

## Why an agent can actually use this

**Every command speaks JSON.** `--json` on any command returns a structured envelope on stdout;
stderr is for progress and diagnostics. Never parse a human-readable string.

**Failures carry a branchable code.** Errors report `error.code` and `error.agent_code`. Branch on the
code, never on the message text:

`agent_code` is the coarse, stable classifier to branch on. `code` is the finer internal
classifier, useful for logging and for picking a more specific remedy.

| `agent_code` | Rolls up `code` | Exit | Retryable | What the agent should do |
| --- | --- | --- | --- | --- |
| `invalid_job` | `invalid_input` | 2 | no | Fix flags, file paths, or job shape |
| `invalid_job` | `validation_failed` | 3 | no | The job failed schema validation |
| `invalid_job` | `invalid_scene` | 3 | no | The scene is invalid, or it rendered nothing visible — check selectors and camera |
| `security_policy` | `runtime_blocked` | 4 | no | Runtime policy, cache, path, timeout, or resource limit blocked it |
| `security_policy` | `security_policy` | 8 | no | Allowlist, offline, path, or host policy rejected it |
| `network_blocked` | `network_error` | 7 | see note | A fetch failed, or an offline cache had no entry |
| `renderer_unavailable` | `renderer_unavailable` | 5 | yes | Node, Mol\*, GL, or canvas is missing — run `doctor` |
| `renderer_unavailable` | `renderer_abi_mismatch` | 5 | yes | Native modules built for the wrong Node ABI — run `update-runtime` |
| `renderer_unavailable` | `render_failed` | 5 | yes | Mol\* loaded the scene but rendering or export failed |
| `renderer_unavailable` | `doctor_failed` | 6 | yes | A capability probe failed — the renderer cannot run |
| `webgl_unavailable` | `renderer_unavailable` | 5 | yes | The renderer exists but cannot create a headless WebGL context |
| `server_busy` | `server_busy` | 9 | yes | Queue is full — retry with backoff, or submit async |
| `canceled` | `canceled` | 130 | no | The request, async job, or job timeout ended the run |
| `internal_error` | `internal_error` | 1 | no | Unclassified — capture the envelope and report it |

`network_blocked` is the one code whose retryability depends on the cause: a transport failure such
as a DNS or connection error is retryable, while a deliberately offline runtime with an empty cache
is not, because retrying cannot help until the environment changes. Branch on `error.retryable`
rather than inferring it from the code.

A blank render is a scene problem, not a renderer problem: it reports `invalid_scene` and is not
retryable, because rerunning the same job cannot fix an empty selection.

**Cost a render before paying for it.** `--dry-run` and `job explain --json` resolve inputs, outputs,
and renderer commands without touching the renderer, so an agent can validate a plan for free.

**Every run is replayable.** Runs are logged and export to portable `.molrun` bundles that another
machine can verify and re-run — which is how you reproduce a failure you didn't witness.

**It diagnoses itself.** `doctor` checks the environment, `diagnose RUN_ID` explains a failure and
suggests the next command, and `diagnose --bundle` writes a redactable zip to hand to a human or
another environment.

The full field-stability policy is in [docs/json-contracts.md](docs/json-contracts.md): which fields
are safe to branch on, and which are diagnostic-only.

## Quickstart

Requires Go 1.22+ and Node 22.

```bash
npm install
npm run build
bin/molstar doctor --json
bin/molstar render examples/1cbs.job.yaml --json
```

No network? Use the built-in fixture:

```bash
bin/molstar render --demo --out demo.png --size 800x600 --json
```

## The agent loop

The default sequence, in full in [docs/agent-workflows.md](docs/agent-workflows.md):

1. `molstar agent doctor --json` — once per environment.
2. `molstar job validate JOB --schema --json` — cheap, catches shape errors.
3. `molstar job explain JOB --json` — resolves what *would* happen.
4. `molstar inspect JOB --semantic=auto --json` — structure counts, refs, camera, representations.
5. `molstar render JOB --json --report render-report.json` — the actual work.
6. `molstar logs export RUN_ID --out run.molrun --json`, then `logs verify` / `logs rerun`.
7. `molstar diagnose RUN_ID --json` when something breaks.

## A job

```yaml
version: 1
runtime:
  cache: .molstar-cache
  network: true
  strict: true
inputs:
  protein:
    id: 1cbs
    provider: pdbe
scene:
  canvas: { background: white }
  structures:
    - ref: protein
      source: protein
      components:
        - ref: polymer
          select: polymer
          representation: { type: cartoon, color: chain }
        - ref: ligand
          select: ligand
          representation: { type: ball-and-stick, color: "#cc3399" }
  camera:
    focus: ligand
outputs:
  - type: image
    path: outputs/1cbs.png
    size: [1600, 1200]
```

`runtime.offline`, `runtime.allow_paths`, and `runtime.allow_hosts` lock a job down for CI or
untrusted input. Full schema: `molstar job schema`, or
[schema/headlessmolstar-job-v1.schema.json](schema/headlessmolstar-job-v1.schema.json).

If YAML by hand is too much, `molstar recipe init ligand --id 1cbs` generates a friendlier recipe that
compiles down to a job.

## Command surface

| | |
| --- | --- |
| **Render** | `render`, `batch`, `bench`, `presets`, `fixtures` |
| **Author** | `recipe`, `job`, `scene`, `selectors` |
| **Understand** | `inspect`, `capabilities`, `examples`, `docs` |
| **Diagnose** | `doctor`, `agent doctor`, `self-test`, `smoke`, `diagnose`, `logs`, `compat` |
| **Serve** | `serve`, `server`, `rpc`, `jobs` |
| **Install** | `install-local`, `install-artifact`, `update-runtime`, `cache`, `version` |

Generated per-command reference lives in [docs/cli/](docs/cli/).

## Server mode

For long-lived workers, skip per-render process startup:

```bash
molstar serve --prewarm --auth-token "$TOKEN"
```

`GET /health`, `GET /ready`, `GET /metrics` (JSON and Prometheus), `POST /render?async=true` with
`GET /jobs/{id}/events` for streaming, and `POST /rpc` for JSON-RPC. Discover the contract with
`molstar serve --openapi`. Check a running server with `molstar serve smoke --url ... --json`.

## Docker

The image bundles the CLI, Node, Mol\*, and the headless GL stack — the fastest way to a working
renderer on Linux.

```bash
docker build -t molstar:local .
docker run --rm molstar:local doctor --json
```

## Client wrappers

Thin subprocess wrappers over the same CLI, for when you want the contract from inside a program:

- **TypeScript** — [src/index.ts](src/index.ts)
- **Python** — [python/](python/)

Both shell out to the binary rather than reimplementing anything, so the JSON contract is identical.

## Docs

- [docs/agent-workflows.md](docs/agent-workflows.md) — the loop, error handling, server mode
- [docs/json-contracts.md](docs/json-contracts.md) — stable vs. diagnostic fields
- [docs/tutorial.md](docs/tutorial.md) — start here if you're a human
- [docs/headless-cookbook.md](docs/headless-cookbook.md) — short task-shaped recipes
- [docs/operator-loop.md](docs/operator-loop.md) — the golden end-to-end operator path
- [docs/platform-support.md](docs/platform-support.md) — contract vs. renderer support per platform
- [docs/release.md](docs/release.md) — release and verification process

## Provenance

Extracted from [sacha-ichbiah/headlessmolstar](https://github.com/sacha-ichbiah/headlessmolstar),
which retains `mdsrv-headless` (MD trajectory server) and a fleet of standalone structural-biology
tool CLIs. Mol\* itself is a separate upstream project — this repo is a headless driver for it, not a
fork of it.
