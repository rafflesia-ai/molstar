## molstar scene compile

Compile a headless job YAML/JSON file to MVSJ

```
molstar scene compile JOB [flags]
```

### Options

```
      --allow-host stringArray   allow remote input host; repeat for multiple hosts
      --allow-path stringArray   allow local input root path; repeat for multiple roots
      --cache string             download cache directory for remote inputs
      --cache-dir string         alias for --cache
  -h, --help                     help for compile
      --json                     write a machine-readable report to stdout
      --max-atoms int            maximum atoms per local/cached structure when countable
      --max-download-bytes int   maximum bytes per cached remote download
      --max-outputs int          maximum number of outputs
      --max-pixels int           maximum pixels per image/video output
      --no-cache                 disable the download cache even if the job file configures one
      --no-network               disable network access
      --offline                  disable network and require cached/local inputs
  -o, --out string               output .mvsj path; use - for stdout
      --profile string           runtime profile: default, ci, or locked
      --timeout int              job timeout in seconds
```

### SEE ALSO

* [molstar scene](molstar_scene.md)	 - Compile and validate scene files
