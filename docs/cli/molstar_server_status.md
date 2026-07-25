## molstar server status

Fetch server job status

```
molstar server status JOB_ID [flags]
```

### Options

```
  -h, --help               help for status
      --json               write JSON output (default true)
      --socket string      connect to a server Unix socket instead of TCP
      --timeout duration   HTTP request timeout (default 30s)
      --token string       bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string         server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar server](molstar_server.md)	 - Submit and manage jobs on a running Mol* server
