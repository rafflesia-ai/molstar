## molstar recipe compile

Compile a recipe to MVSJ/MVSX or a canonical job file

```
molstar recipe compile RECIPE [flags]
```

### Options

```
      --allow-host stringArray   allow remote input host; repeat for multiple hosts
      --allow-path stringArray   allow local input root path; repeat for multiple roots
      --cache string             download cache directory for remote inputs
      --cache-dir string         alias for --cache
  -h, --help                     help for compile
      --json                     write JSON report
      --max-atoms int            maximum atoms per local/cached structure when countable
      --max-download-bytes int   maximum bytes per cached remote download
      --max-outputs int          maximum number of outputs
      --max-pixels int           maximum pixels per image/video output
      --no-cache                 disable the download cache even if the job file configures one
      --no-network               disable network access
      --offline                  disable network and require cached/local inputs
  -o, --out string               output path; .mvsj/.mvsx writes a scene, .json/.yaml writes a job
      --profile string           runtime profile: default, ci, or locked
      --timeout int              job timeout in seconds
```

### SEE ALSO

* [molstar recipe](molstar_recipe.md)	 - Create and compile friendly render recipes
