## molstar serve

Run a local HTTP headless Mol* job server

```
molstar serve [flags]
```

### Options

```
      --addr string               listen address (default "127.0.0.1:8080")
      --allow-host stringArray    allow remote input host; repeat for multiple hosts
      --allow-path stringArray    allow local input root path; repeat for multiple roots
      --auth-token string         protect non-health HTTP endpoints with this bearer token; also supports MOLSTAR_AUTH_TOKEN
      --cache string              download cache directory for remote inputs
      --cache-dir string          alias for --cache
      --dry-run                   return renderer commands without running them
      --foreground-report         print a detailed startup report before serving
  -h, --help                      help for serve
      --job-store string          directory for persisted async job records
      --job-ttl string            prune persisted job records older than this age on startup, e.g. 7d, 24h
      --max-atoms int             maximum atoms per local/cached structure when countable
      --max-download-bytes int    maximum bytes per cached remote download
      --max-outputs int           maximum number of outputs
      --max-pixels int            maximum pixels per image/video output
      --no-cache                  disable the download cache even if the job file configures one
      --no-network                disable network access
      --no-worker                 disable persistent renderer workers and spawn one renderer process per job
      --offline                   disable network and require cached/local inputs
      --openapi                   write the HTTP OpenAPI schema to stdout and exit
      --prewarm                   start the renderer and run a tiny local render probe before accepting traffic
      --profile string            runtime profile: default, ci, or locked
      --queue int                 maximum queued render jobs before returning HTTP 429 (default 16)
      --quiet                     suppress renderer progress logs
      --renderer-command string   renderer command override
      --request-timeout int       per-render request timeout in seconds; 0 uses job/runtime timeout only
      --socket string             listen on a Unix socket instead of TCP
      --timeout int               job timeout in seconds
      --verbose                   show renderer progress logs
      --worker-command string     persistent renderer worker command override
      --workers int               maximum concurrent render jobs (default 1)
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
* [molstar serve smoke](molstar_serve_smoke.md)	 - Smoke-test a running Mol* server
