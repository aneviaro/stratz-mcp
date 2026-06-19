// Package contracts exposes generated public tool contracts and validators.
package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed generated
var generatedFiles embed.FS

// Definition identifies the embedded artifacts for one public tool.
type Definition struct {
	Name                string
	Description         string
	InputSchemaPath     string
	OutputSchemaPath    string
	InputExamplePath    string
	OutputExamplePath   string
	ProtocolFixturePath string
}

// SchemaKind selects a tool input or output schema.
type SchemaKind string

const (
	InputSchema  SchemaKind = "input"
	OutputSchema SchemaKind = "output"
)

type compiledTool struct {
	input  *jsonschema.Schema
	output *jsonschema.Schema
}

var (
	compileOnce sync.Once
	compiled    map[string]compiledTool
	compileErr  error
)

// Definitions returns the generated definitions in deterministic name order.
func Definitions() []Definition {
	return append([]Definition(nil), generatedDefinitions...)
}

// Lookup returns one generated tool definition.
func Lookup(name string) (Definition, bool) {
	for _, definition := range generatedDefinitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return Definition{}, false
}

// Schema returns a copy of an embedded, fully dereferenced schema.
func Schema(name string, kind SchemaKind) ([]byte, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	var path string
	switch kind {
	case InputSchema:
		path = definition.InputSchemaPath
	case OutputSchema:
		path = definition.OutputSchemaPath
	default:
		return nil, fmt.Errorf("unknown schema kind %q", kind)
	}
	data, err := generatedFiles.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s schema for %s: %w", kind, name, err)
	}
	return append([]byte(nil), data...), nil
}

// Example decodes the generated validating example for a tool schema.
func Example(name string, kind SchemaKind) (any, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	var path string
	switch kind {
	case InputSchema:
		path = definition.InputExamplePath
	case OutputSchema:
		path = definition.OutputExamplePath
	default:
		return nil, fmt.Errorf("unknown schema kind %q", kind)
	}
	return decodeEmbedded(path)
}

// ProtocolFixture decodes the generated JSON-RPC request/response fixture.
func ProtocolFixture(name string) (any, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return decodeEmbedded(definition.ProtocolFixturePath)
}

// ValidateInput validates an input value against the generated input schema.
func ValidateInput(name string, value any) error {
	tool, err := compiledFor(name)
	if err != nil {
		return err
	}
	if err := tool.input.Validate(value); err != nil {
		return fmt.Errorf("%s input: %w", name, err)
	}
	return nil
}

// ValidateOutput validates an output value against the generated output schema.
func ValidateOutput(name string, value any) error {
	tool, err := compiledFor(name)
	if err != nil {
		return err
	}
	if err := tool.output.Validate(value); err != nil {
		return fmt.Errorf("%s output: %w", name, err)
	}
	return nil
}

func compiledFor(name string) (compiledTool, error) {
	compileOnce.Do(compileAll)
	if compileErr != nil {
		return compiledTool{}, compileErr
	}
	tool, ok := compiled[name]
	if !ok {
		return compiledTool{}, fmt.Errorf("unknown tool %q", name)
	}
	return tool, nil
}

func compileAll() {
	compiled = make(map[string]compiledTool, len(generatedDefinitions))
	for _, definition := range generatedDefinitions {
		input, err := compileEmbeddedSchema(definition.Name, InputSchema, definition.InputSchemaPath)
		if err != nil {
			compileErr = err
			return
		}
		output, err := compileEmbeddedSchema(definition.Name, OutputSchema, definition.OutputSchemaPath)
		if err != nil {
			compileErr = err
			return
		}
		compiled[definition.Name] = compiledTool{input: input, output: output}
	}
}

func compileEmbeddedSchema(name string, kind SchemaKind, path string) (*jsonschema.Schema, error) {
	data, err := generatedFiles.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s schema for %s: %w", kind, name, err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode %s schema for %s: %w", kind, name, err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	location := fmt.Sprintf("urn:stratz-mcp:embedded:%s:%s", name, kind)
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("load %s schema for %s: %w", kind, name, err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile %s schema for %s: %w", kind, name, err)
	}
	return schema, nil
}

func decodeEmbedded(path string) (any, error) {
	data, err := generatedFiles.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read generated artifact %s: %w", path, err)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode generated artifact %s: %w", path, err)
	}
	return value, nil
}
