## molstar rpc metrics

Fetch server metrics via JSON-RPC

```
molstar rpc metrics [flags]
```

### Options

```
  -h, --help               help for metrics
      --json               write JSON output (default true)
      --socket string      connect to a server Unix socket instead of TCP
      --timeout duration   HTTP request timeout (default 30s)
      --token string       bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string         server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar rpc](molstar_rpc.md)	 - Call a running Mol* server over JSON-RPC
