## molstar render

Render an identifier, job, or MVS scene

```
molstar render INPUT [flags]
```

### Options

```
      --allow-host stringArray     allow remote input host; repeat for multiple hosts
      --allow-path stringArray     allow local input root path; repeat for multiple roots
      --assembly string            assembly identifier
      --background string          canvas background color (default "white")
      --cache string               download cache directory for remote inputs
      --cache-dir string           alias for --cache
      --ci-artifact string         directory for diagnostic artifacts when rendering fails
      --color stringArray          color or high-level theme for matching --select
      --compact                    omit replay-heavy fields from JSON stdout
      --demo                       render a tiny built-in local fixture without network
      --diagnose                   force JSON and include renderer capability diagnostics
      --dry-run                    print external renderer commands without running them
      --explain                    explain the resolved render job without rendering
      --focus string               component ref or selector to focus
      --format string              input format override
  -h, --help                       help for render
      --json                       write a machine-readable report to stdout
      --log-asset-max-bytes int    maximum bytes per embedded run-log asset (default 10485760)
      --log-assets                 embed local input bytes in run logs for replay (default true)
      --log-assets-max-bytes int   maximum total embedded run-log asset bytes (default 52428800)
      --max-atoms int              maximum atoms per local/cached structure when countable
      --max-download-bytes int     maximum bytes per cached remote download
      --max-outputs int            maximum number of outputs
      --max-pixels int             maximum pixels per image/video output
      --mvs string                 write compiled MVSJ scene
      --no-cache                   disable the download cache even if the job file configures one
      --no-log                     do not write a local .molstar-runs report
      --no-network                 disable network access
      --offline                    disable network and require cached/local inputs
      --open                       open the first rendered output with the system viewer
  -o, --out string                 output image/movie path
      --preset string              render preset: default, ligand, polymer, surface, confidence, overview (default "default")
      --profile string             runtime profile: default, ci, or locked
      --provider string            identifier provider: pdbe, rcsb, alphafold (default "pdbe")
      --quiet                      suppress renderer progress logs
      --renderer-command string    renderer command override; defaults to MOLSTAR_RENDER, PATH, or local node_modules/.bin/mvs-render
      --renderer-mode string       renderer mode: subprocess, worker, or auto (default "subprocess")
      --report string              write a machine-readable render report to this path
      --repr stringArray           representation for matching --select
      --run-label string           label stored with the local run log
      --select stringArray         component selector; repeat to add components
      --show-report                print a compact human-readable render summary
      --size string                output size as WIDTHxHEIGHT (default "800x800")
      --state string               write Mol* .molj state next to the rendered image or to this path
      --timeout int                job timeout in seconds
      --verbose                    show renderer progress logs even with --json
      --view string                standard view: front, back, top, bottom, left, right
      --worker-command string      persistent renderer worker command override
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
