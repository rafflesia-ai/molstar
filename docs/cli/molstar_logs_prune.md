## molstar logs prune

Remove old local run logs

```
molstar logs prune [flags]
```

### Options

```
      --dir string          run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR
      --dry-run             print logs that would be removed without deleting
  -h, --help                help for prune
      --json                write machine-readable output
      --older-than string   remove logs older than this age, e.g. 14d, 48h
```

### SEE ALSO

* [molstar logs](molstar_logs.md)	 - Inspect local Mol* CLI run history
