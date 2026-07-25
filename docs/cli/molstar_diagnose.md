## molstar diagnose

Explain a failed run log or CI artifact

```
molstar diagnose RUN_ID [flags]
```

### Options

```
      --bundle                       write a diagnostic issue bundle zip for RUN_ID
      --ci-artifact string           CI artifact directory or ci-artifact.json file
      --dir string                   run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR
  -h, --help                         help for diagnose
      --include-inputs               include embedded run-log input bytes in diagnostic bundles (default true)
      --json                         write machine-readable output
      --max-input-bytes int          maximum total local input bytes to include; 0 disables the limit (default 52428800)
      --max-single-input-bytes int   maximum bytes per local input to include; 0 disables the per-input limit (default 10485760)
  -o, --out string                   diagnostic bundle output path; defaults to RUN_ID.issue.zip
      --redact-env                   redact secret-like environment variable values from diagnostic bundles
      --redact-inputs                omit embedded local input bytes from diagnostic bundles
      --redact-paths                 redact common local filesystem path prefixes from diagnostic bundles
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
