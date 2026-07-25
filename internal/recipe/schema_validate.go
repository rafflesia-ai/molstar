package recipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func ValidateSchemaBytes(data []byte, name string) error {
	instance, err := schemaInstance(data, name)
	if err != nil {
		return err
	}
	schemaDoc, err := schemaDocument()
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(SchemaID, schemaDoc); err != nil {
		return err
	}
	schema, err := compiler.Compile(SchemaID)
	if err != nil {
		return err
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func LoadSchemaBytes(data []byte, name string) (Recipe, error) {
	if err := ValidateSchemaBytes(data, name); err != nil {
		return Recipe{}, err
	}
	return LoadBytes(data, name)
}

func schemaDocument() (any, error) {
	data, err := json.Marshal(JSONSchema())
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

func schemaInstance(data []byte, name string) (any, error) {
	if isJSONPath(name) || json.Valid(data) {
		return jsonschema.UnmarshalJSON(bytes.NewReader(data))
	}
	var decoded any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode recipe yaml: %w", err)
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(normalized))
}

func isJSONPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}
