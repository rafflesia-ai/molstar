## molstar recipe init

Write a starter recipe

```
molstar recipe init [PRESET] [flags]
```

### Options

```
      --format string      output format: yaml or json (default "yaml")
  -h, --help               help for init
      --id string          PDB/AlphaFold-style identifier (default "1cbs")
      --image-out string   image output path stored in the recipe
  -o, --out string         recipe output path; use - for stdout (default "-")
      --path string        local input structure path
      --provider string    identifier provider (default "pdbe")
      --size string        image size as WIDTHxHEIGHT (default "1200x900")
      --url string         remote input structure URL
```

### SEE ALSO

* [molstar recipe](molstar_recipe.md)	 - Create and compile friendly render recipes
