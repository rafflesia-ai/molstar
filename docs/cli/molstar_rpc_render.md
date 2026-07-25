## molstar rpc render

Render a job or recipe via JSON-RPC

```
molstar rpc render JOB [flags]
```

### Options

```
      --async              return as soon as the server accepts the render job
      --dry-run            ask the server to plan rendering without running the renderer
  -h, --help               help for render
      --json               write JSON output (default true)
      --socket string      connect to a server Unix socket instead of TCP
      --timeout duration   HTTP request timeout (default 30s)
      --token string       bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string         server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar rpc](molstar_rpc.md)	 - Call a running Mol* server over JSON-RPC
