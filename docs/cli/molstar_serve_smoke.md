## molstar serve smoke

Smoke-test a running Mol* server

```
molstar serve smoke [flags]
```

### Options

```
  -h, --help                      help for smoke
      --json                      write JSON output (default true)
      --probe-interval duration   poll interval for --render-probe (default 250ms)
      --probe-out-dir string      directory for render-probe files; defaults to a temporary directory
      --probe-timeout duration    maximum time to wait for --render-probe (default 1m0s)
      --render-probe              submit a tiny render job and verify server job lifecycle/output download
      --socket string             connect to a server Unix socket instead of TCP
      --timeout duration          HTTP request timeout (default 30s)
      --token string              bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string                server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar serve](molstar_serve.md)	 - Run a local HTTP headless Mol* job server
