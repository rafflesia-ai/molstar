## molstar batch

Render a JSONL/YAML/JSON batch of jobs

```
molstar batch JOBS [flags]
```

### Options

```
      --allow-host stringArray    allow remote input host; repeat for multiple hosts
      --allow-path stringArray    allow local input root path; repeat for multiple roots
      --cache string              download cache directory for remote inputs
      --cache-dir string          alias for --cache
      --concurrency int           number of jobs to render concurrently (default 1)
      --continue-on-error         continue after a failed job
      --dry-run                   print external renderer commands without running them
  -h, --help                      help for batch
      --json                      write JSON report lines to stdout (default true)
      --manifest string           write a final batch summary JSON manifest to this path
      --max-atoms int             maximum atoms per local/cached structure when countable
      --max-download-bytes int    maximum bytes per cached remote download
      --max-outputs int           maximum number of outputs
      --max-pixels int            maximum pixels per image/video output
      --no-cache                  disable the download cache even if the job file configures one
      --no-network                disable network access
      --offline                   disable network and require cached/local inputs
      --out string                template for image/video outputs, e.g. renders/{id}.png
      --out-dir string            prepend this directory to relative output paths
      --profile string            runtime profile: default, ci, or locked
      --quiet                     suppress renderer progress logs
      --renderer-command string   renderer command override
      --renderer-mode string      renderer mode: subprocess, worker, or auto (default "subprocess")
      --retries int               retry failed jobs up to N additional times
      --skip-existing             skip jobs whose declared outputs already exist and validate
      --timeout int               job timeout in seconds
      --verbose                   show renderer progress logs even with --json
      --worker-command string     persistent renderer worker command override
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
