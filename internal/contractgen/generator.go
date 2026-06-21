// Package contractgen generates the public Go and JSON artifacts from the
// canonical tool contract registry.
package contractgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	contractRegistryPath = "docs/tool-contracts.json"
	expectedContract     = "1.0.0-draft.3"
	expectedProtocol     = "2025-11-25"
	draft202012          = "https://json-schema.org/draft/2020-12/schema"
	referenceCreatedDate = "2026-06-19"
)

var expectedTools = []string{
	"stratz_batch_get_heroes",
	"stratz_batch_get_matches",
	"stratz_batch_get_players",
	"stratz_execute_graphql",
	"stratz_get_constants",
	"stratz_get_hero",
	"stratz_get_hero_stats",
	"stratz_get_league",
	"stratz_get_match",
	"stratz_get_player",
	"stratz_list_league_matches",
	"stratz_list_leagues",
	"stratz_list_live_matches",
	"stratz_list_player_matches",
	"stratz_server_info",
}

type registry struct {
	Schema             string                    `json:"$schema"`
	ID                 string                    `json:"$id"`
	ContractVersion    string                    `json:"contractVersion"`
	MCPProtocolVersion string                    `json:"mcpProtocolVersion"`
	Description        string                    `json:"description"`
	RawGraphQLPolicy   rawGraphQLPolicy          `json:"rawGraphqlPolicy"`
	Defs               map[string]any            `json:"$defs"`
	Tools              map[string]toolDefinition `json:"tools"`
}

type rawGraphQLPolicy struct {
	OperationTypes    map[string]string `json:"operationTypes"`
	RootFieldDefault  string            `json:"rootFieldDefault"`
	AllowedRootFields []string          `json:"allowedRootFields"`
	DeniedRootFields  []string          `json:"deniedRootFields"`
	MetaFields        map[string]string `json:"metaFields"`
	UnknownRootFields string            `json:"unknownRootFields"`
}

type toolDefinition struct {
	Description  string `json:"description"`
	InputSchema  any    `json:"inputSchema"`
	OutputSchema any    `json:"outputSchema"`
}

// Artifact is one deterministic generated file relative to the repository root.
type Artifact struct {
	Path string
	Data []byte
}

// Generate writes every generated contract artifact below root.
func Generate(root string) error {
	artifacts, err := Build(filepath.Join(root, contractRegistryPath))
	if err != nil {
		return err
	}

	generatedDir := filepath.Join(root, "internal/contracts/generated")
	if err := os.RemoveAll(generatedDir); err != nil {
		return fmt.Errorf("clear generated contracts: %w", err)
	}
	for _, artifact := range artifacts {
		path := filepath.Join(root, artifact.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s parent: %w", artifact.Path, err)
		}
		if err := os.WriteFile(path, artifact.Data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", artifact.Path, err)
		}
	}
	return nil
}

// Build validates a registry and returns its complete deterministic artifact set.
func Build(registryPath string) ([]Artifact, error) {
	reg, err := loadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	if err := validateRegistry(reg); err != nil {
		return nil, err
	}

	names := sortedToolNames(reg.Tools)
	artifacts := make([]Artifact, 0, len(names)*5+3)
	manifestTools := make([]map[string]any, 0, len(names))

	for _, name := range names {
		tool := reg.Tools[name]
		input, err := buildSchema(name, "input", tool.InputSchema, reg.Defs)
		if err != nil {
			return nil, err
		}
		output, err := buildSchema(name, "output", tool.OutputSchema, reg.Defs)
		if err != nil {
			return nil, err
		}

		inputExample, err := exampleFor(input)
		if err != nil {
			return nil, fmt.Errorf("%s input example: %w", name, err)
		}
		outputExample, err := exampleFor(output)
		if err != nil {
			return nil, fmt.Errorf("%s output example: %w", name, err)
		}
		if err := validateInstance(name+" input example", input, inputExample); err != nil {
			return nil, err
		}
		if err := validateInstance(name+" output example", output, outputExample); err != nil {
			return nil, err
		}

		inputSchemaPath := "internal/contracts/generated/schemas/" + name + ".input.json"
		outputSchemaPath := "internal/contracts/generated/schemas/" + name + ".output.json"
		inputExamplePath := "internal/contracts/generated/examples/" + name + ".input.json"
		outputExamplePath := "internal/contracts/generated/examples/" + name + ".output.json"
		protocolPath := "internal/contracts/generated/protocol/" + name + ".json"

		inputJSON, err := marshalJSON(input)
		if err != nil {
			return nil, err
		}
		outputJSON, err := marshalJSON(output)
		if err != nil {
			return nil, err
		}
		inputExampleJSON, err := marshalJSON(inputExample)
		if err != nil {
			return nil, err
		}
		outputExampleJSON, err := marshalJSON(outputExample)
		if err != nil {
			return nil, err
		}
		protocolJSON, err := protocolFixture(name, inputExample, outputExample)
		if err != nil {
			return nil, err
		}

		artifacts = append(artifacts,
			Artifact{Path: inputSchemaPath, Data: inputJSON},
			Artifact{Path: outputSchemaPath, Data: outputJSON},
			Artifact{Path: inputExamplePath, Data: inputExampleJSON},
			Artifact{Path: outputExamplePath, Data: outputExampleJSON},
			Artifact{Path: protocolPath, Data: protocolJSON},
		)
		manifestTools = append(manifestTools, map[string]any{
			"name":           name,
			"input_schema":   strings.TrimPrefix(inputSchemaPath, "internal/contracts/"),
			"output_schema":  strings.TrimPrefix(outputSchemaPath, "internal/contracts/"),
			"input_example":  strings.TrimPrefix(inputExamplePath, "internal/contracts/"),
			"output_example": strings.TrimPrefix(outputExamplePath, "internal/contracts/"),
			"protocol":       strings.TrimPrefix(protocolPath, "internal/contracts/"),
		})
	}

	manifest, err := marshalJSON(map[string]any{
		"contract_version":     reg.ContractVersion,
		"mcp_protocol_version": reg.MCPProtocolVersion,
		"schema_draft":         reg.Schema,
		"tool_count":           len(names),
		"tools":                manifestTools,
	})
	if err != nil {
		return nil, err
	}
	goSource, err := renderGo(reg, names)
	if err != nil {
		return nil, err
	}

	artifacts = append(artifacts,
		Artifact{Path: "internal/contracts/generated/manifest.json", Data: manifest},
		Artifact{Path: "internal/contracts/zz_generated.contracts.go", Data: goSource},
		Artifact{Path: "docs/generated-tool-contracts.md", Data: renderReference(reg, names)},
	)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func loadRegistry(path string) (registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return registry{}, fmt.Errorf("read contract registry: %w", err)
	}
	var reg registry
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&reg); err != nil {
		return registry{}, fmt.Errorf("decode contract registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return registry{}, errors.New("decode contract registry: trailing JSON values")
		}
		return registry{}, fmt.Errorf("decode contract registry trailing data: %w", err)
	}
	return reg, nil
}

func validateRegistry(reg registry) error {
	if reg.Schema != draft202012 {
		return fmt.Errorf("contract registry uses %q, want %q", reg.Schema, draft202012)
	}
	if reg.ContractVersion != expectedContract {
		return fmt.Errorf("contract version %q is unsupported; generator expects %q", reg.ContractVersion, expectedContract)
	}
	if reg.MCPProtocolVersion != expectedProtocol {
		return fmt.Errorf("MCP protocol version %q is unsupported; generator expects %q", reg.MCPProtocolVersion, expectedProtocol)
	}
	if reg.ID == "" {
		return errors.New("contract registry $id is required")
	}
	if len(reg.Defs) == 0 {
		return errors.New("contract registry $defs must not be empty")
	}
	if err := validateRawGraphQLPolicy(reg.RawGraphQLPolicy); err != nil {
		return err
	}

	names := sortedToolNames(reg.Tools)
	if !slices.Equal(names, expectedTools) {
		return fmt.Errorf("tool registry mismatch: got %v, want %v", names, expectedTools)
	}
	for name, definition := range reg.Defs {
		if err := validateSchemaShape("$defs."+name, definition); err != nil {
			return err
		}
	}
	for _, name := range names {
		tool := reg.Tools[name]
		if strings.TrimSpace(tool.Description) == "" {
			return fmt.Errorf("%s description is required", name)
		}
		if err := validateSchemaShape(name+".inputSchema", tool.InputSchema); err != nil {
			return err
		}
		if err := validateSchemaShape(name+".outputSchema", tool.OutputSchema); err != nil {
			return err
		}
		if err := compileSourceSchema(name+".inputSchema", tool.InputSchema, reg.Defs); err != nil {
			return err
		}
		if err := compileSourceSchema(name+".outputSchema", tool.OutputSchema, reg.Defs); err != nil {
			return err
		}
	}
	return nil
}

func validateRawGraphQLPolicy(policy rawGraphQLPolicy) error {
	if policy.OperationTypes["query"] != "allow_subject_to_root_policy" ||
		policy.OperationTypes["mutation"] != "deny" ||
		policy.OperationTypes["subscription"] != "deny" {
		return errors.New("raw GraphQL operation policy is invalid")
	}
	if policy.RootFieldDefault != "deny" ||
		policy.UnknownRootFields != "deny_until_policy_revision" {
		return errors.New("raw GraphQL root policy must be default-deny")
	}
	if policy.MetaFields["__typename"] != "allow" ||
		policy.MetaFields["__schema"] != "allow_only_with_runtime_introspection_flag" ||
		policy.MetaFields["__type"] != "allow_only_with_runtime_introspection_flag" {
		return errors.New("raw GraphQL meta-field policy is invalid")
	}
	if len(policy.AllowedRootFields) == 0 {
		return errors.New("raw GraphQL allowed root fields must not be empty")
	}
	if !slices.IsSorted(policy.AllowedRootFields) ||
		!slices.IsSorted(policy.DeniedRootFields) {
		return errors.New("raw GraphQL root field lists must be sorted")
	}
	seen := make(map[string]string)
	for classification, fields := range map[string][]string{
		"allowed": policy.AllowedRootFields,
		"denied":  policy.DeniedRootFields,
	} {
		for _, field := range fields {
			if strings.TrimSpace(field) == "" {
				return errors.New("raw GraphQL root field names must not be empty")
			}
			if previous, exists := seen[field]; exists {
				return fmt.Errorf(
					"raw GraphQL root field %q is both %s and %s",
					field,
					previous,
					classification,
				)
			}
			seen[field] = classification
		}
	}
	return nil
}

var supportedKeywords = map[string]struct{}{
	"$comment": {}, "$defs": {}, "$id": {}, "$ref": {}, "$schema": {},
	"additionalProperties": {}, "allOf": {}, "anyOf": {}, "const": {},
	"default": {}, "deprecated": {}, "description": {}, "else": {}, "enum": {},
	"examples": {}, "exclusiveMaximum": {}, "exclusiveMinimum": {}, "format": {},
	"if": {}, "items": {}, "maxItems": {}, "maxLength": {}, "maxProperties": {},
	"maximum": {}, "minItems": {}, "minLength": {}, "minProperties": {}, "minimum": {},
	"multipleOf": {}, "not": {}, "oneOf": {}, "pattern": {}, "properties": {},
	"readOnly": {}, "required": {}, "then": {}, "title": {}, "type": {},
	"uniqueItems": {}, "writeOnly": {},
}

func validateSchemaShape(path string, schema any) error {
	switch node := schema.(type) {
	case bool:
		return nil
	case map[string]any:
		for key := range node {
			if _, ok := supportedKeywords[key]; !ok {
				return fmt.Errorf("%s uses unsupported JSON Schema keyword %q", path, key)
			}
		}
		if ref, ok := node["$ref"]; ok {
			value, ok := ref.(string)
			if !ok || !strings.HasPrefix(value, "#/$defs/") {
				return fmt.Errorf("%s has unsupported $ref %v", path, ref)
			}
			if len(node) != 1 {
				return fmt.Errorf("%s uses unsupported $ref siblings", path)
			}
		}
		for _, key := range []string{"properties", "$defs"} {
			if children, ok := node[key].(map[string]any); ok {
				for childName, child := range children {
					if err := validateSchemaShape(path+"."+key+"."+childName, child); err != nil {
						return err
					}
				}
			}
		}
		for _, key := range []string{"additionalProperties", "items", "not", "if", "then", "else"} {
			if child, ok := node[key]; ok {
				if _, isSchema := child.(map[string]any); isSchema {
					if err := validateSchemaShape(path+"."+key, child); err != nil {
						return err
					}
				} else if _, isBool := child.(bool); !isBool && (key == "additionalProperties" || key == "items") {
					return fmt.Errorf("%s.%s must be a schema", path, key)
				}
			}
		}
		for _, key := range []string{"allOf", "anyOf", "oneOf"} {
			if children, ok := node[key].([]any); ok {
				for i, child := range children {
					if err := validateSchemaShape(fmt.Sprintf("%s.%s[%d]", path, key, i), child); err != nil {
						return err
					}
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s must be a JSON Schema object or boolean", path)
	}
}

func compileSourceSchema(label string, schema any, defs map[string]any) error {
	root, ok := clone(schema).(map[string]any)
	if !ok {
		if schema == true {
			root = map[string]any{}
		} else {
			return fmt.Errorf("%s cannot be a false schema", label)
		}
	}
	root["$schema"] = draft202012
	root["$defs"] = clone(defs)
	return compileSchema(label, root)
}

func compileSchema(label string, schema any) error {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	location := "urn:stratz-contract:" + strings.NewReplacer(".", "-", "/", "-").Replace(label)
	if err := compiler.AddResource(location, schema); err != nil {
		return fmt.Errorf("%s schema resource: %w", label, err)
	}
	if _, err := compiler.Compile(location); err != nil {
		return fmt.Errorf("%s is not valid Draft 2020-12 JSON Schema: %w", label, err)
	}
	return nil
}

func buildSchema(toolName, direction string, source any, defs map[string]any) (map[string]any, error) {
	value, err := dereference(source, defs, nil)
	if err != nil {
		return nil, fmt.Errorf("%s %s schema: %w", toolName, direction, err)
	}
	schema, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s %s schema must be an object", toolName, direction)
	}
	schema["$schema"] = draft202012
	schema["$id"] = fmt.Sprintf("urn:stratz-mcp:contract:%s:%s:%s", expectedContract, toolName, direction)
	// MCP Tool inputSchema and outputSchema are required to describe JSON
	// objects. Some contract outputs express this through oneOf branches; make
	// the common root explicit for SDK interoperability without changing the
	// accepted instances.
	if _, present := schema["type"]; !present {
		schema["type"] = "object"
	}
	if containsRef(schema) {
		return nil, fmt.Errorf("%s %s schema still contains $ref after dereferencing", toolName, direction)
	}
	if err := compileSchema(toolName+"."+direction, schema); err != nil {
		return nil, err
	}
	return schema, nil
}

func dereference(value any, defs map[string]any, stack []string) (any, error) {
	switch node := value.(type) {
	case map[string]any:
		if rawRef, ok := node["$ref"]; ok {
			ref, ok := rawRef.(string)
			if !ok || !strings.HasPrefix(ref, "#/$defs/") {
				return nil, fmt.Errorf("unsupported reference %v", rawRef)
			}
			name := strings.TrimPrefix(ref, "#/$defs/")
			if slices.Contains(stack, name) {
				return nil, fmt.Errorf("cyclic definition reference: %s -> %s", strings.Join(stack, " -> "), name)
			}
			definition, ok := defs[name]
			if !ok {
				return nil, fmt.Errorf("unknown definition %q", name)
			}
			return dereference(clone(definition), defs, append(stack, name))
		}
		result := make(map[string]any, len(node))
		for key, child := range node {
			if key == "$defs" {
				continue
			}
			resolved, err := dereference(child, defs, stack)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(node))
		for i, child := range node {
			resolved, err := dereference(child, defs, stack)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	default:
		return node, nil
	}
}

func containsRef(value any) bool {
	switch node := value.(type) {
	case map[string]any:
		if _, ok := node["$ref"]; ok {
			return true
		}
		for _, child := range node {
			if containsRef(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if containsRef(child) {
				return true
			}
		}
	}
	return false
}

func clone(value any) any {
	switch node := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(node))
		for key, child := range node {
			result[key] = clone(child)
		}
		return result
	case []any:
		result := make([]any, len(node))
		for i, child := range node {
			result[i] = clone(child)
		}
		return result
	default:
		return node
	}
}

func exampleFor(schema any) (any, error) {
	effective, err := effectiveSchema(schema)
	if err != nil {
		return nil, err
	}
	switch node := effective.(type) {
	case bool:
		if !node {
			return nil, errors.New("cannot generate an example for a false schema")
		}
		return nil, nil
	case map[string]any:
		if value, ok := node["const"]; ok {
			return clone(value), nil
		}
		if values, ok := node["enum"].([]any); ok && len(values) > 0 {
			return clone(values[0]), nil
		}
		if alternatives, ok := node["oneOf"].([]any); ok && len(alternatives) > 0 {
			return exampleFor(alternatives[0])
		}
		if alternatives, ok := node["anyOf"].([]any); ok && len(alternatives) > 0 {
			return exampleFor(alternatives[0])
		}

		types := schemaTypes(node)
		if slices.Contains(types, "null") {
			return nil, nil
		}
		var schemaType string
		if len(types) > 0 {
			schemaType = types[0]
		} else if _, ok := node["properties"]; ok {
			schemaType = "object"
		}
		switch schemaType {
		case "object":
			required := stringSet(node["required"])
			properties, _ := node["properties"].(map[string]any)
			result := make(map[string]any, len(required))
			names := make([]string, 0, len(required))
			for name := range required {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				property, ok := properties[name]
				if !ok {
					return nil, fmt.Errorf("required property %q has no schema", name)
				}
				value, err := exampleFor(property)
				if err != nil {
					return nil, fmt.Errorf("property %s: %w", name, err)
				}
				result[name] = value
			}
			return result, nil
		case "array":
			count := integerKeyword(node, "minItems", 0)
			items, ok := node["items"]
			if !ok {
				items = true
			}
			result := make([]any, count)
			for i := range result {
				value, err := exampleFor(items)
				if err != nil {
					return nil, err
				}
				result[i] = value
			}
			return result, nil
		case "string":
			switch node["format"] {
			case "date-time":
				return "2026-01-01T00:00:00Z", nil
			case "uri":
				return "https://example.invalid/", nil
			}
			switch node["pattern"] {
			case "^[0-9]{1,10}$", "^[0-9]{1,20}$":
				return "1", nil
			case "^[0-9]{17,20}$":
				return "76561198000000000", nil
			}
			minimum := integerKeyword(node, "minLength", 1)
			maximum := integerKeyword(node, "maxLength", max(minimum, 7))
			length := min(max(minimum, 1), maximum)
			if length <= 7 {
				return strings.Repeat("x", length), nil
			}
			return strings.Repeat("x", length), nil
		case "integer":
			minimum := numberKeyword(node, "minimum", 0)
			return int64(math.Ceil(minimum)), nil
		case "number":
			return numberKeyword(node, "minimum", 0), nil
		case "boolean":
			return false, nil
		case "":
			return nil, nil
		default:
			return nil, fmt.Errorf("unsupported example type %q", schemaType)
		}
	default:
		return nil, fmt.Errorf("invalid schema node %T", effective)
	}
}

func effectiveSchema(schema any) (any, error) {
	node, ok := schema.(map[string]any)
	if !ok {
		return schema, nil
	}
	allOf, ok := node["allOf"].([]any)
	if !ok {
		return node, nil
	}
	base := make(map[string]any, len(node)-1)
	for key, value := range node {
		if key != "allOf" {
			base[key] = clone(value)
		}
	}
	for _, part := range allOf {
		effective, err := effectiveSchema(part)
		if err != nil {
			return nil, err
		}
		partMap, ok := effective.(map[string]any)
		if !ok {
			return nil, errors.New("allOf generation only supports object schemas")
		}
		base = mergeSchemas(base, partMap)
	}
	return base, nil
}

func mergeSchemas(left, right map[string]any) map[string]any {
	result := clone(left).(map[string]any)
	for key, value := range right {
		switch key {
		case "properties":
			target, _ := result[key].(map[string]any)
			if target == nil {
				target = map[string]any{}
			}
			source, _ := value.(map[string]any)
			for name, property := range source {
				if target[name] == true {
					target[name] = clone(property)
				} else {
					target[name] = clone(property)
				}
			}
			result[key] = target
		case "required":
			set := stringSet(result[key])
			for name := range stringSet(value) {
				set[name] = struct{}{}
			}
			names := make([]string, 0, len(set))
			for name := range set {
				names = append(names, name)
			}
			sort.Strings(names)
			values := make([]any, len(names))
			for i, name := range names {
				values[i] = name
			}
			result[key] = values
		default:
			result[key] = clone(value)
		}
	}
	return result
}

func validateInstance(label string, schema map[string]any, instance any) error {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	location := "urn:stratz-example:" + strings.ReplaceAll(label, " ", "-")
	if err := compiler.AddResource(location, schema); err != nil {
		return fmt.Errorf("%s resource: %w", label, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return fmt.Errorf("%s schema: %w", label, err)
	}
	if err := compiled.Validate(instance); err != nil {
		return fmt.Errorf("%s does not validate: %w", label, err)
	}
	return nil
}

func protocolFixture(name string, input, output any) ([]byte, error) {
	compactOutput, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("%s compact output: %w", name, err)
	}
	fixture := map[string]any{
		"request": map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": input,
			},
		},
		"response": map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"content": []any{
					map[string]any{
						"type": "text",
						"text": string(compactOutput),
					},
				},
				"structuredContent": output,
			},
		},
	}
	return marshalJSON(fixture)
}

func marshalJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal generated JSON: %w", err)
	}
	return append(data, '\n'), nil
}

func renderGo(reg registry, names []string) ([]byte, error) {
	var builder strings.Builder
	builder.WriteString("// Code generated by contractgen; DO NOT EDIT.\n\n")
	builder.WriteString("package contracts\n\n")
	fmt.Fprintf(&builder, "const ContractVersion = %q\n", reg.ContractVersion)
	fmt.Fprintf(&builder, "const MCPProtocolVersion = %q\n", reg.MCPProtocolVersion)
	fmt.Fprintf(&builder, "const SchemaDraft = %q\n\n", reg.Schema)
	renderStringSliceFunction(
		&builder,
		"RawGraphQLAllowedRootFields",
		reg.RawGraphQLPolicy.AllowedRootFields,
	)
	renderStringSliceFunction(
		&builder,
		"RawGraphQLDeniedRootFields",
		reg.RawGraphQLPolicy.DeniedRootFields,
	)
	builder.WriteString("type ToolResult[T any] struct {\n")
	builder.WriteString("\tKind string `json:\"kind\"`\n")
	builder.WriteString("\tData *T `json:\"data,omitempty\"`\n")
	builder.WriteString("\tSummary *string `json:\"summary,omitempty\"`\n")
	builder.WriteString("\tProvenance *Provenance `json:\"provenance,omitempty\"`\n")
	builder.WriteString("\tWarnings []string `json:\"warnings,omitempty\"`\n")
	builder.WriteString("\tRaw any `json:\"raw,omitempty\"`\n")
	builder.WriteString("\tError *Error `json:\"error,omitempty\"`\n")
	builder.WriteString("\tContext any `json:\"context,omitempty\"`\n")
	builder.WriteString("}\n\n")

	if errorSchema, ok := reg.Defs["error"].(map[string]any); ok {
		if properties, ok := errorSchema["properties"].(map[string]any); ok {
			if codeSchema, ok := properties["code"].(map[string]any); ok {
				renderStringEnum(&builder, "ErrorCode", codeSchema)
			}
		}
	}

	defNames := make([]string, 0, len(reg.Defs))
	for name := range reg.Defs {
		defNames = append(defNames, name)
	}
	sort.Strings(defNames)
	for _, name := range defNames {
		renderNamedType(&builder, goName(name), reg.Defs[name], goName(name))
	}

	for _, name := range names {
		tool := reg.Tools[name]
		prefix := goName(name)
		renderNamedType(&builder, prefix+"Request", tool.InputSchema, prefix+"Request")
		dataSchema, err := successDataSchema(tool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("%s response type: %w", name, err)
		}
		renderNamedType(&builder, prefix+"Data", dataSchema, prefix+"Data")
		fmt.Fprintf(&builder, "type %sResponse ToolResult[%sData]\n\n", prefix, prefix)
	}

	builder.WriteString("var generatedDefinitions = []Definition{\n")
	for _, name := range names {
		tool := reg.Tools[name]
		fmt.Fprintf(&builder, "\t{Name: %q, Description: %q, InputSchemaPath: %q, OutputSchemaPath: %q, InputExamplePath: %q, OutputExamplePath: %q, ProtocolFixturePath: %q},\n",
			name,
			tool.Description,
			"generated/schemas/"+name+".input.json",
			"generated/schemas/"+name+".output.json",
			"generated/examples/"+name+".input.json",
			"generated/examples/"+name+".output.json",
			"generated/protocol/"+name+".json",
		)
	}
	builder.WriteString("}\n")

	formatted, err := format.Source([]byte(builder.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go contracts: %w\n%s", err, builder.String())
	}
	return formatted, nil
}

func renderStringSliceFunction(builder *strings.Builder, name string, values []string) {
	fmt.Fprintf(builder, "func %s() []string {\n", name)
	builder.WriteString("\treturn []string{\n")
	for _, value := range values {
		fmt.Fprintf(builder, "\t\t%q,\n", value)
	}
	builder.WriteString("\t}\n")
	builder.WriteString("}\n\n")
}

func renderNamedType(builder *strings.Builder, name string, schema any, context string) {
	if name == "NullableDateTime" {
		builder.WriteString("type NullableDateTime = *DateTime\n\n")
		return
	}
	if node, ok := schema.(map[string]any); ok {
		if isStringEnum(node) {
			renderStringEnum(builder, name, node)
			return
		}
	}
	fmt.Fprintf(builder, "type %s %s\n\n", name, goType(schema, context, 0))
}

func renderStringEnum(builder *strings.Builder, name string, schema map[string]any) {
	fmt.Fprintf(builder, "type %s string\n\n", name)
	values, _ := schema["enum"].([]any)
	if len(values) == 0 {
		return
	}
	builder.WriteString("const (\n")
	for _, raw := range values {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		fmt.Fprintf(builder, "\t%s%s %s = %q\n", name, enumName(value), name, value)
	}
	builder.WriteString(")\n\n")
}

func isStringEnum(schema map[string]any) bool {
	types := schemaTypes(schema)
	_, ok := schema["enum"].([]any)
	return ok && len(types) == 1 && types[0] == "string"
}

func goType(schema any, context string, indent int) string {
	switch node := schema.(type) {
	case bool:
		return "any"
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			return goName(strings.TrimPrefix(ref, "#/$defs/"))
		}
		if value, ok := node["const"]; ok {
			switch value.(type) {
			case string:
				return "string"
			case bool:
				return "bool"
			case float64:
				return "float64"
			}
		}
		if alternatives, ok := node["oneOf"].([]any); ok {
			if len(alternatives) == 1 {
				return goType(alternatives[0], context, indent)
			}
			if base, ok := nullableAlternative(alternatives); ok {
				return pointerType(goType(base, context, indent))
			}
			return "any"
		}
		types := schemaTypes(node)
		if len(types) > 1 {
			nonNull := make([]string, 0, len(types))
			for _, schemaType := range types {
				if schemaType != "null" {
					nonNull = append(nonNull, schemaType)
				}
			}
			if len(nonNull) == 1 {
				copyNode := clone(node).(map[string]any)
				copyNode["type"] = nonNull[0]
				return pointerType(goType(copyNode, context, indent))
			}
			return "any"
		}
		var schemaType string
		if len(types) == 1 {
			schemaType = types[0]
		} else if _, ok := node["properties"]; ok {
			schemaType = "object"
		}
		switch schemaType {
		case "string":
			return "string"
		case "integer":
			return "int64"
		case "number":
			return "float64"
		case "boolean":
			return "bool"
		case "array":
			return "[]" + goType(node["items"], context+"Item", indent)
		case "object":
			if properties, ok := node["properties"].(map[string]any); ok {
				return goStruct(properties, stringSet(node["required"]), context, indent)
			}
			if additional, ok := node["additionalProperties"]; ok {
				if allowed, isBool := additional.(bool); isBool {
					if !allowed {
						return "struct{}"
					}
					return "map[string]any"
				}
				return "map[string]" + goType(additional, context+"Value", indent)
			}
			return "map[string]any"
		default:
			return "any"
		}
	default:
		return "any"
	}
}

func goStruct(properties map[string]any, required map[string]struct{}, context string, indent int) string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "struct{}"
	}
	var builder strings.Builder
	builder.WriteString("struct {\n")
	for _, name := range names {
		fieldType := goType(properties[name], context+goName(name), indent+1)
		_, isRequired := required[name]
		if context == "Error" && name == "code" {
			fieldType = "ErrorCode"
		} else if !isRequired {
			fieldType = optionalType(fieldType)
		}
		tag := name
		if !isRequired {
			tag += ",omitempty"
		}
		fmt.Fprintf(&builder, "%s%s %s `json:%s`\n", strings.Repeat("\t", indent+1), goName(name), fieldType, strconv.Quote(tag))
	}
	builder.WriteString(strings.Repeat("\t", indent))
	builder.WriteString("}")
	return builder.String()
}

func optionalType(value string) string {
	if value == "any" || strings.HasPrefix(value, "[]") || strings.HasPrefix(value, "map[") || strings.HasPrefix(value, "*") || strings.HasPrefix(value, "Nullable") {
		return value
	}
	return "*" + value
}

func pointerType(value string) string {
	if value == "any" || strings.HasPrefix(value, "*") {
		return value
	}
	return "*" + value
}

func nullableAlternative(alternatives []any) (any, bool) {
	var base any
	foundNull := false
	for _, alternative := range alternatives {
		node, ok := alternative.(map[string]any)
		if ok && slices.Contains(schemaTypes(node), "null") {
			foundNull = true
			continue
		}
		if base != nil {
			return nil, false
		}
		base = alternative
	}
	return base, foundNull && base != nil
}

func successDataSchema(output any) (any, error) {
	node, ok := output.(map[string]any)
	if !ok {
		return nil, errors.New("output schema is not an object")
	}
	alternatives, ok := node["oneOf"].([]any)
	if !ok || len(alternatives) == 0 {
		return nil, errors.New("output schema has no success oneOf branch")
	}
	success, ok := alternatives[0].(map[string]any)
	if !ok {
		return nil, errors.New("success branch is not an object")
	}
	allOf, ok := success["allOf"].([]any)
	if !ok {
		return nil, errors.New("success branch has no allOf")
	}
	for _, part := range allOf {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		properties, ok := partMap["properties"].(map[string]any)
		if !ok {
			continue
		}
		if data, ok := properties["data"]; ok {
			return data, nil
		}
	}
	return nil, errors.New("success branch has no data schema")
}

func renderReference(reg registry, names []string) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "---\nCreated: %s\nPurpose: Generated reference for the public STRATZ MCP tool contracts.\nStatus: Generated from docs/tool-contracts.json; do not edit manually\n---\n\n", referenceCreatedDate)
	builder.WriteString("# Generated STRATZ MCP tool contracts\n\n")
	fmt.Fprintf(&builder, "- Contract version: `%s`\n", reg.ContractVersion)
	fmt.Fprintf(&builder, "- MCP protocol version: `%s`\n", reg.MCPProtocolVersion)
	fmt.Fprintf(&builder, "- JSON Schema dialect: `%s`\n", reg.Schema)
	fmt.Fprintf(&builder, "- Tool count: `%d`\n\n", len(names))
	builder.WriteString("| Tool | Description | Required input fields |\n")
	builder.WriteString("|---|---|---|\n")
	for _, name := range names {
		tool := reg.Tools[name]
		required := requiredFields(tool.InputSchema)
		requiredText := "None"
		if len(required) > 0 {
			quoted := make([]string, len(required))
			for i, field := range required {
				quoted[i] = "`" + field + "`"
			}
			requiredText = strings.Join(quoted, ", ")
		}
		description := strings.ReplaceAll(tool.Description, "|", "\\|")
		fmt.Fprintf(&builder, "| `%s` | %s | %s |\n", name, description, requiredText)
	}
	builder.WriteString("\n## Tool details\n\n")
	for _, name := range names {
		tool := reg.Tools[name]
		fmt.Fprintf(&builder, "### `%s`\n\n%s\n\n", name, tool.Description)
		input, _ := tool.InputSchema.(map[string]any)
		properties, _ := input["properties"].(map[string]any)
		required := stringSet(input["required"])
		if len(properties) == 0 {
			builder.WriteString("Inputs: none.\n\n")
		} else {
			builder.WriteString("| Input | Required | Type and constraints |\n")
			builder.WriteString("|---|---:|---|\n")
			for _, field := range sortedMapKeys(properties) {
				_, isRequired := required[field]
				fmt.Fprintf(
					&builder,
					"| `%s` | %t | %s |\n",
					field,
					isRequired,
					schemaSummary(properties[field]),
				)
			}
			builder.WriteString("\n")
		}
		fmt.Fprintf(
			&builder,
			"Artifacts: [input schema](../internal/contracts/generated/schemas/%s.input.json), [output schema](../internal/contracts/generated/schemas/%s.output.json), [examples](../internal/contracts/generated/examples/%s.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/%s.json).\n\n",
			name,
			name,
			name,
			name,
		)
	}
	builder.WriteString("All outputs use the generated success/error envelope. The linked output schemas are authoritative for payload shapes, bounds, and tool-specific error details.\n")
	return []byte(builder.String())
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func schemaSummary(value any) string {
	node, ok := value.(map[string]any)
	if !ok {
		return "See schema."
	}
	parts := []string{}
	if reference, ok := node["$ref"].(string); ok {
		parts = append(parts, "`"+strings.TrimPrefix(reference, "#/$defs/")+"`")
	} else if types := schemaTypes(node); len(types) > 0 {
		parts = append(parts, "`"+strings.Join(types, " | ")+"`")
	}
	if values, ok := node["enum"].([]any); ok {
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, fmt.Sprintf("`%v`", value))
		}
		parts = append(parts, "one of "+strings.Join(quoted, ", "))
	}
	for _, keyword := range []string{"default", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"} {
		if value, exists := node[keyword]; exists {
			parts = append(parts, fmt.Sprintf("%s `%v`", keyword, value))
		}
	}
	if len(parts) == 0 {
		return "See schema."
	}
	return strings.Join(parts, "; ")
}

func requiredFields(schema any) []string {
	node, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	set := stringSet(node["required"])
	result := make([]string, 0, len(set))
	for name := range set {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func sortedToolNames(tools map[string]toolDefinition) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schemaTypes(schema map[string]any) []string {
	switch value := schema["type"].(type) {
	case string:
		return []string{value}
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if item, ok := item.(string); ok {
				result = append(result, item)
			}
		}
		return result
	default:
		return nil
	}
}

func stringSet(value any) map[string]struct{} {
	result := map[string]struct{}{}
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				result[text] = struct{}{}
			}
		}
	case []string:
		for _, value := range values {
			result[value] = struct{}{}
		}
	}
	return result
}

func integerKeyword(schema map[string]any, name string, fallback int) int {
	switch value := schema[name].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	default:
		return fallback
	}
}

func numberKeyword(schema map[string]any, name string, fallback float64) float64 {
	switch value := schema[name].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return fallback
	}
}

var initialisms = map[string]string{
	"api": "API", "cgo": "CGO", "graphql": "GraphQL", "http": "HTTP",
	"id": "ID", "json": "JSON", "lru": "LRU", "mcp": "MCP", "sdk": "SDK",
	"sqlite": "SQLite", "tls": "TLS", "ttl": "TTL", "uri": "URI",
	"url": "URL", "waf": "WAF",
}

var wordBoundary = regexp.MustCompile(`[^A-Za-z0-9]+`)

func goName(value string) string {
	separated := wordBoundary.Split(value, -1)
	parts := make([]string, 0, len(separated))
	for _, part := range separated {
		parts = append(parts, splitIdentifier(part)...)
	}
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if initialism, ok := initialisms[lower]; ok {
			builder.WriteString(initialism)
			continue
		}
		if strings.HasPrefix(lower, "id") && len(lower) > 2 {
			if _, err := strconv.Atoi(lower[2:]); err == nil {
				builder.WriteString("ID")
				builder.WriteString(lower[2:])
				continue
			}
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		for i := 1; i < len(runes); i++ {
			runes[i] = unicode.ToLower(runes[i])
		}
		builder.WriteString(string(runes))
	}
	if builder.Len() == 0 {
		return "Value"
	}
	return builder.String()
}

func splitIdentifier(value string) []string {
	if value == "" {
		return nil
	}
	runes := []rune(value)
	start := 0
	var parts []string
	for i := 1; i < len(runes); i++ {
		previous := runes[i-1]
		current := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		split := unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous))
		split = split || (unicode.IsUpper(previous) && unicode.IsUpper(current) && next != 0 && unicode.IsLower(next))
		if split {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

func enumName(value string) string {
	name := goName(strings.ToLower(value))
	if name == "" {
		return "Value"
	}
	return name
}
