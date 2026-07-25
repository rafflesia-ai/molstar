## molstar server submit

Submit a job or recipe to a running server

```
molstar server submit JOB [flags]
```

### Options

```
      --download-outputs        download completed output files when used with --wait
      --dry-run                 ask the server to plan rendering without running the renderer
  -h, --help                    help for submit
      --interval duration       poll interval for --wait (default 500ms)
      --json                    write JSON output (default true)
      --out-dir string          directory for --download-outputs (default ".")
      --socket string           connect to a server Unix socket instead of TCP
      --timeout duration        HTTP request timeout (default 30s)
      --token string            bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string              server base URL (default "http://127.0.0.1:8080")
      --wait                    wait for the submitted job to finish
      --wait-timeout duration   maximum time to wait with --wait; 0 waits forever
```

### SEE ALSO

* [molstar server](molstar_server.md)	 - Submit and manage jobs on a running Mol* server
