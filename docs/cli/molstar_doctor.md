## molstar doctor

Check local headless rendering prerequisites

```
molstar doctor [flags]
```

### Options

```
      --cache string        cache directory created by --fix
      --config string       runtime config path written by --fix
      --fix                 repair local renderer config and missing npm dependencies
  -h, --help                help for doctor
      --home string         source checkout/runtime root used by --fix
      --json                write a machine-readable report to stdout
      --probe-size string   render probe size as WIDTHxHEIGHT (default "64x64")
      --skip-probe          skip the local GL/canvas render probe
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
