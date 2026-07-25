## molstar bench

Benchmark the headless renderer with a local fixture, identifier, structure, job, or recipe

```
molstar bench [INPUT] [flags]
```

### Options

```
      --allow-host stringArray         allow remote input host; repeat for multiple hosts
      --allow-path stringArray         allow local input root path; repeat for multiple roots
      --assembly string                assembly identifier
      --background string              canvas background color (default "white")
      --baseline string                previous benchmark JSON snapshot to compare against
      --cache string                   download cache directory for remote inputs
      --cache-dir string               alias for --cache
      --continue-on-error              continue running iterations after a failed render
      --demo                           benchmark the built-in local fixture without network
      --dry-run                        plan benchmark renders without running the renderer
      --fail-regression string         maximum allowed avg/p95 regression when --baseline is set; accepts values like 20 or 20%
      --format string                  input format override
  -h, --help                           help for bench
      --iterations int                 measured render iterations (default 3)
      --json                           write a machine-readable benchmark report
      --label string                   label for the benchmark snapshot
      --max-atoms int                  maximum atoms per local/cached structure when countable
      --max-download-bytes int         maximum bytes per cached remote download
      --max-outputs int                maximum number of outputs
      --max-pixels int                 maximum pixels per image/video output
      --max-regression-percent float   maximum allowed avg/p95 regression when --baseline is set (default 25)
      --no-cache                       disable the download cache even if the job file configures one
      --no-network                     disable network access
      --offline                        disable network and require cached/local inputs
      --out-dir string                 directory for benchmark artifacts; defaults to a temporary directory
      --preset string                  render preset: default, ligand, polymer, surface, confidence, overview (default "default")
      --profile string                 runtime profile: default, ci, or locked
      --provider string                identifier provider: pdbe, rcsb, alphafold (default "pdbe")
      --quiet                          suppress renderer progress logs
      --renderer-command string        renderer command override
      --renderer-mode string           renderer mode: subprocess, worker, or auto (default "subprocess")
      --report string                  write the benchmark report to this JSON path
      --size string                    output size as WIDTHxHEIGHT (default "256x256")
      --timeout int                    job timeout in seconds
      --verbose                        show renderer progress logs
      --warmup int                     warmup iterations before measurement (default 1)
      --worker-command string          persistent renderer worker command override
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
