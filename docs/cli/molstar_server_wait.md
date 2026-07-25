## molstar server wait

Wait for a server job to finish

```
molstar server wait JOB_ID [flags]
```

### Options

```
      --download-outputs           download completed output files from the server
  -h, --help                       help for wait
      --interval duration          poll interval (default 500ms)
      --json                       write JSON output (default true)
      --out-dir string             directory for --download-outputs (default ".")
      --request-timeout duration   per-HTTP-request timeout (default 30s)
      --socket string              connect to a server Unix socket instead of TCP
      --timeout duration           maximum time to wait; 0 waits forever
      --token string               bearer auth token; defaults to MOLSTAR_AUTH_TOKEN
      --url string                 server base URL (default "http://127.0.0.1:8080")
```

### SEE ALSO

* [molstar server](molstar_server.md)	 - Submit and manage jobs on a running Mol* server
