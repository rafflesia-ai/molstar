# Headless Mol* tutorial

## 1. Check the runtime

From a checkout:

```bash
npm install
go run ./cmd/molstar doctor --json
```

After installing the CLI locally:

```bash
molstar doctor --json
molstar self-test --json
```

## 2. Render without network

Use the built-in fixture when validating a machine or CI image:

```bash
molstar render --demo --out demo.png --size 800x600 --json
```

For a fast command contract check that does not invoke the renderer:

```bash
molstar render --demo --dry-run --out demo.png --size 160x120 --json
```

## 3. Start from a recipe

Create and validate a friendly YAML recipe:

```bash
molstar recipe init ligand --id 1cbs --out ligand.recipe.yaml
molstar recipe validate ligand.recipe.yaml --schema --json
molstar recipe explain ligand.recipe.yaml --schema --json
```

Render it:

```bash
molstar render ligand.recipe.yaml --out ligand.png --size 1600x1200 --json
```

Compile it to the canonical job and MVS artifacts:

```bash
molstar recipe compile ligand.recipe.yaml --out ligand.job.yaml --json
molstar scene compile ligand.job.yaml --out ligand.mvsj --json
```

## 4. Use selectors and presets

Inspect supported selectors:

```bash
molstar selectors list
molstar selectors explain 'chain:A/residue:10-20' --json
```

Render with direct flags:

```bash
molstar render 1cbs \
  --select polymer --repr cartoon --color chain \
  --select ligand --repr ball-and-stick --color '#cc3399' \
  --focus ligand \
  --size 1600x1200 \
  --out 1cbs.png \
  --json
```

Use a preset when the defaults are enough:

```bash
molstar render 1cbs --preset ligand --json
molstar presets list
molstar presets show ligand --json
```

## 5. Batch jobs

Create a JSONL file where each line is a job or recipe-compatible object, then run:

```bash
molstar batch jobs.jsonl --concurrency 4 --continue-on-error --json
```

For repeated renders on one machine, use the worker renderer:

```bash
molstar batch jobs.jsonl --concurrency 4 --renderer-mode worker --json
```

## 6. Install a packaged artifact

Create a local artifact:

```bash
npm run package:local
```

Install it elsewhere:

```bash
molstar install-artifact \
  --artifact dist/headlessmolstar-local-$(go env GOOS)-$(go env GOARCH).tar.gz \
  --bin-dir "$HOME/.local/bin" \
  --force \
  --json
```

The installer writes a runtime config so the installed binary can find its bundled renderer scripts and Node dependencies.

## 7. Python wrapper

Add `python/` to `PYTHONPATH` or install the folder as a local package, then call the same CLI contract:

```python
from headlessmolstar import HeadlessMolstar, demo_job

molstar = HeadlessMolstar("molstar", timeout=180)
print(molstar.version(runtime=False)["cli"]["version"])

report = molstar.render(
    demo_job("python-demo.png", size=(96, 72)),
    dry_run=True,
    renderer_mode="auto",
)
assert report["ok"]
```

For real rendering, remove `dry_run=True` and use local paths or runtime cache/allowlist settings appropriate for the job.

## 8. Benchmark a renderer

Capture a local snapshot:

```bash
npm run bench:snapshot
```

Compare to a previous report:

```bash
BASELINE=outputs/benchmarks/previous/bench.json npm run bench:snapshot
```
