// Package schema implements the authenticated, local-only STRATZ schema lifecycle.
package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	OperationName = "StratzSchemaIntrospection"
	FormatVersion = 1
)

// IntrospectionQuery is named, read-only, and uses the same bounded production
// HTTP contract as every other curated STRATZ request.
const IntrospectionQuery = `query StratzSchemaIntrospection {
  __schema {
    description
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind
      name
      description
      specifiedByURL
      fields(includeDeprecated: true) {
        name
        description
        args(includeDeprecated: true) {
          name
          description
          type { ...TypeRef }
          defaultValue
          isDeprecated
          deprecationReason
        }
        type { ...TypeRef }
        isDeprecated
        deprecationReason
      }
      inputFields(includeDeprecated: true) {
        name
        description
        type { ...TypeRef }
        defaultValue
        isDeprecated
        deprecationReason
      }
      interfaces { ...TypeRef }
      enumValues(includeDeprecated: true) {
        name
        description
        isDeprecated
        deprecationReason
      }
      possibleTypes { ...TypeRef }
    }
    directives {
      name
      description
      isRepeatable
      locations
      args(includeDeprecated: true) {
        name
        description
        type { ...TypeRef }
        defaultValue
        isDeprecated
        deprecationReason
      }
    }
  }
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType {
        kind
        name
        ofType {
          kind
          name
          ofType {
            kind
            name
            ofType {
              kind
              name
              ofType {
                kind
                name
              }
            }
          }
        }
      }
    }
  }
}`

// Fetch obtains one complete introspection result through the bounded STRATZ
// executor. The caller controls where the restricted result is persisted.
func Fetch(ctx context.Context, executor stratz.Executor) (Document, error) {
	if executor == nil {
		return Document{}, errors.New("schema introspection requires a STRATZ executor")
	}
	budget, err := stratz.NewRequestBudget(1)
	if err != nil {
		return Document{}, err
	}
	response, err := executor.Execute(ctx, budget, stratz.Request{
		Query:         IntrospectionQuery,
		OperationName: OperationName,
		Variables:     map[string]any{},
		Mode:          stratz.ModeCurated,
		AllowRetries:  false,
	})
	if err != nil {
		return Document{}, fmt.Errorf("execute authenticated schema introspection: %w", err)
	}
	if response == nil || len(response.Data) == 0 {
		return Document{}, errors.New("schema introspection returned no data")
	}

	var envelope struct {
		Schema *Document `json:"__schema"`
	}
	if err := json.Unmarshal(response.Data, &envelope); err != nil {
		return Document{}, fmt.Errorf("decode schema introspection data: %w", err)
	}
	if envelope.Schema == nil {
		return Document{}, errors.New("schema introspection omitted __schema")
	}
	if err := envelope.Schema.Validate(); err != nil {
		return Document{}, err
	}
	return *envelope.Schema, nil
}

type Document struct {
	Description      *string     `json:"description,omitempty"`
	QueryType        *NamedType  `json:"queryType"`
	MutationType     *NamedType  `json:"mutationType,omitempty"`
	SubscriptionType *NamedType  `json:"subscriptionType,omitempty"`
	Types            []Type      `json:"types"`
	Directives       []Directive `json:"directives"`
}

type NamedType struct {
	Name string `json:"name"`
}

type Type struct {
	Kind          string       `json:"kind"`
	Name          string       `json:"name"`
	Description   *string      `json:"description,omitempty"`
	SpecifiedBy   *string      `json:"specifiedByURL,omitempty"`
	Fields        []Field      `json:"fields,omitempty"`
	InputFields   []InputValue `json:"inputFields,omitempty"`
	Interfaces    []TypeRef    `json:"interfaces,omitempty"`
	EnumValues    []EnumValue  `json:"enumValues,omitempty"`
	PossibleTypes []TypeRef    `json:"possibleTypes,omitempty"`
}

type Field struct {
	Name              string       `json:"name"`
	Description       *string      `json:"description,omitempty"`
	Args              []InputValue `json:"args"`
	Type              TypeRef      `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason,omitempty"`
}

type InputValue struct {
	Name              string  `json:"name"`
	Description       *string `json:"description,omitempty"`
	Type              TypeRef `json:"type"`
	DefaultValue      *string `json:"defaultValue,omitempty"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason,omitempty"`
}

type EnumValue struct {
	Name              string  `json:"name"`
	Description       *string `json:"description,omitempty"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason,omitempty"`
}

type Directive struct {
	Name         string       `json:"name"`
	Description  *string      `json:"description,omitempty"`
	IsRepeatable bool         `json:"isRepeatable"`
	Locations    []string     `json:"locations"`
	Args         []InputValue `json:"args"`
}

type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name,omitempty"`
	OfType *TypeRef `json:"ofType,omitempty"`
}

func (document Document) Validate() error {
	if document.QueryType == nil || document.QueryType.Name == "" {
		return errors.New("schema introspection has no query type")
	}
	if len(document.Types) == 0 {
		return errors.New("schema introspection has no types")
	}
	for _, definition := range document.Types {
		if definition.Name == "" || definition.Kind == "" {
			return errors.New("schema introspection contains an unnamed type")
		}
	}
	return nil
}
