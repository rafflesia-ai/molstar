## molstar install-artifact

Install a packaged headless Mol* artifact

```
molstar install-artifact [flags]
```

### Options

```
      --artifact string   artifact tar.gz, zip, or unpacked runtime directory
      --bin-dir string    directory to install the molstar binary into
      --config string     config path; defaults to XDG config or ~/.config/molstar/config.json
      --force             overwrite existing executable or runtime directory
  -h, --help              help for install-artifact
      --install-deps      run npm install when renderer dependencies are missing (default true)
      --json              write a machine-readable report
      --name string       installed executable name (default "molstar")
      --prefix string     runtime install directory; defaults to XDG_DATA_HOME/molstar/runtime
```

### SEE ALSO

* [molstar](molstar.md)	 - Headless Mol* job runner
