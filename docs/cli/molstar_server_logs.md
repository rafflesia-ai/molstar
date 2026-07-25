## molstar server logs

Show a readable server job timeline

```
molstar server logs JOB_ID [flags]
```

### Options

```
  -h, --help                       help for logs
      --json                       write JSON output
      --request-timeout duration   per-HTTP-request timeout (default 30s)
      --socket string              connect to a server Unix socket instead of TCP
      --token string               bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string                 server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar server](molstar_server.md)	 - Submit and manage jobs on a running Mol* server
