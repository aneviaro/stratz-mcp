package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const endpoint = "https://api.stratz.com/graphql"

type response struct {
	Data struct {
		Type struct {
			Fields []field `json:"fields"`
		} `json:"__type"`
	} `json:"data"`
	Errors []map[string]any `json:"errors"`
}

type typeInfo struct {
	Kind        string       `json:"kind"`
	Name        *string      `json:"name"`
	Fields      []field      `json:"fields"`
	InputFields []inputValue `json:"inputFields"`
	EnumValues  []struct {
		Name string `json:"name"`
	} `json:"enumValues"`
}

type field struct {
	Name string  `json:"name"`
	Args []arg   `json:"args"`
	Type typeRef `json:"type"`
}

type inputValue struct {
	Name string  `json:"name"`
	Type typeRef `json:"type"`
}

type arg struct {
	Name string  `json:"name"`
	Type typeRef `json:"type"`
}

type typeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name"`
	OfType *typeRef `json:"ofType"`
}

type signature struct {
	Name   string   `json:"name"`
	Args   []string `json:"args"`
	Result string   `json:"result"`
}

type typeSummary struct {
	Kind        string      `json:"kind"`
	Fields      []signature `json:"fields,omitempty"`
	InputFields []string    `json:"input_fields,omitempty"`
	EnumValues  []string    `json:"enum_values,omitempty"`
}

func main() {
	token := strings.TrimSpace(os.Getenv("STRATZ_API_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr, "STRATZ_API_TOKEN is not set")
		os.Exit(2)
	}

	query := `query RootSignatures {
  __type(name: "DotaQuery") {
    fields(includeDeprecated: true) {
      name
      args {
        name
        type {
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
      type {
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
}`

	payload, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": "RootSignatures",
		"variables":     map[string]any{},
	})
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/graphql-response+json, application/json")
	req.Header.Set("User-Agent", "stratz-mcp-schema-inspect/0.1 (+https://github.com/aneviaro/stratz-mcp)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("unexpected status %s", resp.Status))
	}

	var result response
	if err := json.Unmarshal(body, &result); err != nil {
		fatal(err)
	}
	if len(result.Errors) > 0 {
		fatal(fmt.Errorf("introspection returned %d GraphQL errors", len(result.Errors)))
	}

	signatures := make([]signature, 0, len(result.Data.Type.Fields))
	for _, field := range result.Data.Type.Fields {
		args := make([]string, 0, len(field.Args))
		for _, arg := range field.Args {
			args = append(args, arg.Name+": "+formatType(arg.Type))
		}
		signatures = append(signatures, signature{
			Name:   field.Name,
			Args:   args,
			Result: formatType(field.Type),
		})
	}
	sort.Slice(signatures, func(i, j int) bool {
		return signatures[i].Name < signatures[j].Name
	})

	typeNames := []string{
		"ConstantQuery",
		"HeroMatchupType",
		"HeroPositionTimeDetailType",
		"HeroStatsQuery",
		"HeroType",
		"LeagueMatchesRequestType",
		"LeagueRequestType",
		"LeagueType",
		"LiveQuery",
		"MatchLiveRequestType",
		"MatchLiveType",
		"MatchPlaybackDataType",
		"MatchPlayerType",
		"MatchType",
		"PlayerMatchesRequestType",
		"PlayerType",
	}
	if filter := strings.TrimSpace(os.Getenv("STRATZ_SCHEMA_TYPES")); filter != "" {
		typeNames = nil
		for _, name := range strings.Split(filter, ",") {
			if name = strings.TrimSpace(name); name != "" {
				typeNames = append(typeNames, name)
			}
		}
	}

	typeSummaries, err := inspectTypes(token, typeNames)
	if err != nil {
		fatal(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{
		"root_fields": signatures,
		"types":       typeSummaries,
	}); err != nil {
		fatal(err)
	}
}

func inspectTypes(token string, names []string) (map[string]typeSummary, error) {
	var query strings.Builder
	query.WriteString("query TypeSignatures {\n")
	for i, name := range names {
		fmt.Fprintf(&query, "t%d: __type(name: %q) { ...TypeInfo }\n", i, name)
	}
	query.WriteString(`}
fragment TypeInfo on __Type {
  kind
  name
  fields(includeDeprecated: true) {
    name
    args {
      name
      type {
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
    type {
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
  inputFields {
    name
    type {
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
  enumValues(includeDeprecated: true) {
    name
  }
}`)

	payload, err := json.Marshal(map[string]any{
		"query":         query.String(),
		"operationName": "TypeSignatures",
		"variables":     map[string]any{},
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/graphql-response+json, application/json")
	req.Header.Set("User-Agent", "stratz-mcp-schema-inspect/0.1 (+https://github.com/aneviaro/stratz-mcp)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("type introspection returned %s", resp.Status)
	}

	var result struct {
		Data   map[string]typeInfo `json:"data"`
		Errors []map[string]any    `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("type introspection returned %d GraphQL errors", len(result.Errors))
	}

	summaries := make(map[string]typeSummary, len(names))
	for i, requestedName := range names {
		info := result.Data[fmt.Sprintf("t%d", i)]
		summary := typeSummary{Kind: info.Kind}
		for _, field := range info.Fields {
			args := make([]string, 0, len(field.Args))
			for _, arg := range field.Args {
				args = append(args, arg.Name+": "+formatType(arg.Type))
			}
			summary.Fields = append(summary.Fields, signature{
				Name:   field.Name,
				Args:   args,
				Result: formatType(field.Type),
			})
		}
		sort.Slice(summary.Fields, func(i, j int) bool {
			return summary.Fields[i].Name < summary.Fields[j].Name
		})
		for _, inputField := range info.InputFields {
			summary.InputFields = append(summary.InputFields, inputField.Name+": "+formatType(inputField.Type))
		}
		sort.Strings(summary.InputFields)
		for _, enumValue := range info.EnumValues {
			summary.EnumValues = append(summary.EnumValues, enumValue.Name)
		}
		sort.Strings(summary.EnumValues)
		summaries[requestedName] = summary
	}
	return summaries, nil
}

func formatType(ref typeRef) string {
	switch ref.Kind {
	case "NON_NULL":
		if ref.OfType == nil {
			return "<invalid>!"
		}
		return formatType(*ref.OfType) + "!"
	case "LIST":
		if ref.OfType == nil {
			return "[<invalid>]"
		}
		return "[" + formatType(*ref.OfType) + "]"
	default:
		if ref.Name == nil {
			return "<anonymous>"
		}
		return *ref.Name
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "schema inspection failed:", err)
	os.Exit(1)
}
