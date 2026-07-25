## molstar inspect

Inspect a job's inputs, selections, and planned outputs before rendering

```
molstar inspect JOB [flags]
```

### Options

```
      --allow-host stringArray     allow remote input host; repeat for multiple hosts
      --allow-path stringArray     allow local input root path; repeat for multiple roots
      --cache string               download cache directory for remote inputs
      --cache-dir string           alias for --cache
  -h, --help                       help for inspect
      --json                       write JSON report (default true)
      --max-atoms int              maximum atoms per local/cached structure when countable
      --max-download-bytes int     maximum bytes per cached remote download
      --max-outputs int            maximum number of outputs
      --max-pixels int             maximum pixels per image/video output
      --no-cache                   disable the download cache even if the job file configures one
      --no-network                 disable network access
      --no-prepare                 skip runtime preparation and remote cache resolution
      --offline                    disable network and require cached/local inputs
      --profile string             runtime profile: default, ci, or locked
      --renderer-command string    renderer command override for semantic inspection
      --select string              additional selector to inspect
      --semantic string[="true"]   semantic inspection mode: auto, true, or false (default "auto")
      --strict-semantic            fail if Mol* semantic inspection cannot run
      --timeout int                job timeout in seconds
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
