## molstar logs export

Export a run log bundle

```
molstar logs export RUN_ID [flags]
```

### Options

```
      --dir string                   run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR
  -h, --help                         help for export
      --include-inputs               embed local input files in the exported bundle (default true)
      --json                         write machine-readable output
      --max-input-bytes int          maximum total local input bytes to embed; 0 disables the limit (default 52428800)
      --max-single-input-bytes int   maximum bytes per local input to embed; 0 disables the per-input limit (default 10485760)
  -o, --out string                   output .molrun bundle
```

### SEE ALSO

* [molstar logs](molstar_logs.md)	 - Inspect local Mol* CLI run history
