package job

func JSONSchema() map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  SchemaID,
		"$comment":             "HeadlessMolstar job schema v1 is append-only within version:1. Unknown fields are rejected by schema validation. Breaking changes require version:2.",
		"title":                "Headless Molstar Job",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"version", "inputs", "scene"},
		"properties": map[string]any{
			"version": map[string]any{"const": 1},
			"runtime": ref("runtime"),
			"inputs": map[string]any{
				"type":                 "object",
				"additionalProperties": ref("input"),
				"minProperties":        1,
			},
			"scene":   ref("scene"),
			"outputs": arrayOf(ref("output")),
			"assets":  arrayOf(ref("asset")),
		},
		"$defs": map[string]any{
			"runtime": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"cache":              stringSchema(),
					"profile":            enumString("default", "ci", "locked"),
					"network":            map[string]any{"type": "boolean"},
					"offline":            map[string]any{"type": "boolean"},
					"strict":             map[string]any{"type": "boolean"},
					"timeout_seconds":    positiveInteger(),
					"max_pixels":         positiveInteger(),
					"max_atoms":          positiveInteger(),
					"max_outputs":        positiveInteger(),
					"max_download_bytes": positiveInteger(),
					"max_archive_bytes":  positiveInteger(),
					"allow_hosts":        arrayOf(stringSchema()),
					"allow_paths":        arrayOf(stringSchema()),
				},
			},
			"input": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"oneOf": []any{
					map[string]any{"required": []string{"id"}},
					map[string]any{"required": []string{"path"}},
					map[string]any{"required": []string{"url"}},
				},
				"properties": map[string]any{
					"id":       stringSchema(),
					"provider": enumString("pdbe", "rcsb", "alphafold", "afdb"),
					"path":     stringSchema(),
					"url":      stringSchema(),
					"format":   stringSchema(),
					"assembly": stringSchema(),
				},
			},
			"scene": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"structures"},
				"properties": map[string]any{
					"canvas":     ref("canvas"),
					"structures": arrayOf(ref("structure")),
					"camera":     ref("camera"),
				},
			},
			"canvas": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"background": stringSchema(),
				},
			},
			"structure": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"source"},
				"properties": map[string]any{
					"ref":        stringSchema(),
					"source":     stringSchema(),
					"type":       enumString("model", "assembly", "symmetry", "symmetry_mates"),
					"assembly":   stringSchema(),
					"components": arrayOf(ref("component")),
				},
			},
			"component": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"select"},
				"properties": map[string]any{
					"ref":            stringSchema(),
					"select":         stringSchema(),
					"representation": ref("representation"),
					"label":          stringSchema(),
					"tooltip":        stringSchema(),
				},
			},
			"representation": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"type":             enumString("cartoon", "ball-and-stick", "ball_and_stick", "spacefill", "line", "surface", "backbone"),
					"color":            stringSchema(),
					"size_factor":      map[string]any{"type": "number", "exclusiveMinimum": 0},
					"ignore_hydrogens": map[string]any{"type": "boolean"},
				},
			},
			"camera": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"focus":     stringSchema(),
					"view":      enumString("front", "back", "top", "bottom", "left", "right"),
					"zoom":      map[string]any{"type": "number", "exclusiveMinimum": 0},
					"target":    vector3(),
					"position":  vector3(),
					"up":        vector3(),
					"direction": vector3(),
					"near":      map[string]any{"type": "number", "exclusiveMinimum": 0},
				},
			},
			"output": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"type", "path"},
				"properties": map[string]any{
					"type":        enumString("image", "png", "jpg", "jpeg", "video", "mp4", "mvsj", "mvsx", "molj", "state"),
					"path":        stringSchema(),
					"size":        vector2(),
					"transparent": map[string]any{"type": "boolean"},
					"quality":     enumString("low", "medium", "high"),
				},
			},
			"asset": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name", "path"},
				"properties": map[string]any{
					"name": stringSchema(),
					"path": stringSchema(),
				},
			},
		},
	}
}

func SchemaInfo() map[string]any {
	return map[string]any{
		"ok":               true,
		"schema":           "job-v1",
		"schema_id":        SchemaID,
		"job_version":      1,
		"compatibility":    "append-only within version:1; breaking changes require a new job version",
		"unknown_fields":   "rejected when job validate --schema is used",
		"current_policy":   "version:1 is the only accepted job version",
		"runtime_profiles": []string{"default", "ci", "locked"},
	}
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/$defs/" + name}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func enumString(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func positiveInteger() map[string]any {
	return map[string]any{"type": "integer", "minimum": 1}
}

func arrayOf(items any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func vector2() map[string]any {
	return map[string]any{
		"type":     "array",
		"minItems": 2,
		"maxItems": 2,
		"items":    positiveInteger(),
	}
}

func vector3() map[string]any {
	return map[string]any{
		"type":     "array",
		"minItems": 3,
		"maxItems": 3,
		"items":    map[string]any{"type": "number"},
	}
}
