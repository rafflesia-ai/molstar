## molstar jobs prune

Prune persisted server job records

```
molstar jobs prune [flags]
```

### Options

```
      --dry-run            print records without removing them
  -h, --help               help for prune
      --job-store string   persisted job store directory (default ".molstar-jobs")
      --json               write JSON report
      --ttl string         remove jobs older than this duration (default "24h")
```

### SEE ALSO

* [molstar jobs](molstar_jobs.md)	 - Manage persisted server jobs
