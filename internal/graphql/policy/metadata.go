package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

type metadataSchemaPolicy struct {
	metadata schema.ValidationMetadata
}

// FromManifest builds demand-control rules from authenticated schema metadata.
func FromManifest(manifest schema.Manifest) (SchemaPolicy, error) {
	if strings.TrimSpace(manifest.Validation.QueryType) == "" ||
		len(manifest.Validation.Fields) == 0 {
		return nil, fmt.Errorf("schema validation metadata is incomplete")
	}
	return metadataSchemaPolicy{metadata: manifest.Validation}, nil
}

func (policy metadataSchemaPolicy) Field(path []string, field *ast.Field) FieldRule {
	typeName := policy.metadata.QueryType
	var reference schema.Ref
	found := false
	for _, segment := range path {
		fields := policy.metadata.Fields[typeName]
		reference, found = fields[segment]
		if !found {
			if len(field.SelectionSet) > 0 {
				return FieldRule{Kind: FieldList, Cacheable: false}
			}
			return FieldRule{Kind: FieldScalar, Cacheable: false}
		}
		typeName = baseType(reference.Type)
	}
	rule := FieldRule{
		Cacheable: true,
	}
	switch {
	case reference.ListDepth > 0:
		rule.Kind = FieldList
		rule.PageSizeArguments = policy.pageSizeArguments(reference)
	case len(policy.metadata.Fields[typeName]) > 0:
		rule.Kind = FieldObject
	default:
		rule.Kind = FieldScalar
	}
	lower := strings.ToLower(field.Name)
	if strings.Contains(lower, "email") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "steamaccount") ||
		lower == "identity" {
		rule.Sensitive = true
		rule.Cacheable = false
	}
	return rule
}

func (policy metadataSchemaPolicy) pageSizeArguments(reference schema.Ref) []string {
	var result []string
	for name, argument := range reference.Arguments {
		lower := strings.ToLower(name)
		if argument.ListDepth > 0 ||
			lower == "take" || lower == "first" || lower == "limit" {
			result = append(result, name)
			continue
		}
		inputFields := policy.metadata.Fields[baseType(argument.Type)]
		for _, candidate := range []string{"take", "first", "limit"} {
			if _, ok := inputFields[candidate]; ok {
				result = append(result, name+"."+candidate)
			}
		}
	}
	sort.Strings(result)
	return result
}

func baseType(value string) string {
	return strings.Trim(value, "[]!")
}
