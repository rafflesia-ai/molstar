## molstar job normalize

Print a canonical JSON/YAML job spec from flags or a job file

```
molstar job normalize INPUT [flags]
```

### Options

```
      --allow-host stringArray   allow remote input host; repeat for multiple hosts
      --allow-path stringArray   allow local input root path; repeat for multiple roots
      --assembly string          assembly identifier
      --background string        canvas background color (default "white")
      --cache string             download cache directory for remote inputs
      --cache-dir string         alias for --cache
      --color stringArray        color or high-level theme for matching --select
      --focus string             component ref or selector to focus
      --format string            output format: json or yaml (default "json")
      --format-input string      input structure format override
  -h, --help                     help for normalize
      --json                     force the normalized job format to JSON and write JSON errors if the command fails
      --max-atoms int            maximum atoms per local/cached structure when countable
      --max-download-bytes int   maximum bytes per cached remote download
      --max-outputs int          maximum number of outputs
      --max-pixels int           maximum pixels per image/video output
      --no-cache                 disable the download cache even if the job file configures one
      --no-network               disable network access
      --offline                  disable network and require cached/local inputs
  -o, --out string               image/movie output path to place in the normalized job
      --preset string            render preset: default, ligand, polymer, surface, confidence, overview (default "default")
      --profile string           runtime profile: default, ci, or locked
      --provider string          identifier provider: pdbe, rcsb, alphafold (default "pdbe")
      --repr stringArray         representation for matching --select
      --select stringArray       component selector; repeat to add components
      --size string              output size as WIDTHxHEIGHT (default "800x800")
      --timeout int              job timeout in seconds
      --view string              standard view: front, back, top, bottom, left, right
      --write string             normalized job output path; use - for stdout (default "-")
```

### SEE ALSO

* [molstar job](molstar_job.md)	 - Inspect and generate headless job specs
