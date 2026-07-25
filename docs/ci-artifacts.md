# CI Artifact Convention

Use this pattern when a CI render should produce a shareable issue bundle on failure.

```bash
export MOLSTAR_RUNS_DIR="$RUNNER_TEMP/molstar-runs"
mkdir -p "$MOLSTAR_RUNS_DIR" "$RUNNER_TEMP/molstar-ci"

set +e
molstar render job.yaml \
  --run-label ci-render \
  --ci-artifact "$RUNNER_TEMP/molstar-ci" \
  --json >"$RUNNER_TEMP/molstar-render.json"
status="$?"
set -e

if [ "$status" -ne 0 ]; then
  run_id="$(molstar logs --last --dir "$MOLSTAR_RUNS_DIR" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])')"
  ./scripts/collect-molstar-ci-artifact.sh \
    --run-id "$run_id" \
    --runs-dir "$MOLSTAR_RUNS_DIR" \
    --ci-artifact "$RUNNER_TEMP/molstar-ci" \
    --out-dir "$RUNNER_TEMP/molstar-issues"
fi

exit "$status"
```

GitHub Actions upload step:

```yaml
- uses: actions/upload-artifact@v4
  if: failure()
  with:
    name: molstar-render-issue
    path: |
      ${{ runner.temp }}/molstar-render.json
      ${{ runner.temp }}/molstar-ci
      ${{ runner.temp }}/molstar-issues
```

The collector defaults to `--redact-paths --redact-env --redact-inputs`. Use `--keep-inputs` only for private CI artifacts where replayability is more important than sharing safety.

When `--out-dir` is used, bundles are named `molstar-RUN_ID.issue.zip`. The collector writes:

- `*.issue.zip`: the diagnostic bundle
- `*.issue.zip.json`: the `molstar diagnose --bundle --json` report
- `*.issue.zip.manifest.json`: schema, run id, redaction policy, CI artifact path, and retention hint

Recommended retention:

- Pull requests: 14 days
- Main branch failures: 30 days
- Release candidates: 90 days
