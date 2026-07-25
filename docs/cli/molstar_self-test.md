## molstar self-test

Run an end-to-end local CLI smoke test

```
molstar self-test [flags]
```

### Options

```
  -h, --help               help for self-test
      --json               write a machine-readable report to stdout
      --keep               keep generated self-test artifacts
      --out-dir string     directory for temporary self-test artifacts
      --require-worker     fail when the persistent worker renderer cannot render the demo
      --timeout duration   overall self-test timeout (default 2m0s)
      --verbose            include child command stdout/stderr and parsed reports
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
