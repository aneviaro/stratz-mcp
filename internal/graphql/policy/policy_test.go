package policy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestOperationSelectionAndRootPolicy(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		operationName string
		maxOperations int
		wantCode      contracts.ErrorCode
	}{
		{name: "syntax", query: `query {`, wantCode: contracts.ErrorCodeInvalidArgument},
		{
			name:     "operation limit",
			query:    `query A { match(id: 1) { id } } query B { team(teamId: 1) { id } }`,
			wantCode: contracts.ErrorCodeQueryOperationLimitExceeded,
		},
		{
			name:          "unknown operation name",
			query:         `query A { match(id: 1) { id } }`,
			operationName: "B",
			wantCode:      contracts.ErrorCodeInvalidArgument,
		},
		{
			name:          "ambiguous without name",
			query:         `query A { match(id: 1) { id } } query B { team(teamId: 1) { id } }`,
			maxOperations: 2,
			wantCode:      contracts.ErrorCodeInvalidArgument,
		},
		{
			name:          "named selection",
			query:         `query A { match(id: 1) { id } } query B { team(teamId: 1) { id } }`,
			operationName: "B",
			maxOperations: 2,
		},
		{name: "mutation", query: `mutation { match(id: 1) { id } }`, wantCode: contracts.ErrorCodeQueryOperationNotAllowed},
		{name: "subscription", query: `subscription { live { __typename } }`, wantCode: contracts.ErrorCodeQueryOperationNotAllowed},
		{name: "explicitly denied", query: `query { hidden: plus { __typename } }`, wantCode: contracts.ErrorCodeQueryOperationNotAllowed},
		{name: "unknown root", query: `query { futureRoot { id } }`, wantCode: contracts.ErrorCodeQueryOperationNotAllowed},
		{name: "allowed alias", query: `query { hidden: match(id: 1) { id } }`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := testLimits()
			if test.maxOperations != 0 {
				limits.MaxGraphQLOperations = test.maxOperations
			}
			checker := mustPolicy(t, Options{Limits: limits})
			analysis, err := checker.Analyze(Request{
				Query:         test.query,
				OperationName: test.operationName,
			})
			assertPolicyCode(t, err, test.wantCode)
			if test.wantCode == "" && analysis == nil {
				t.Fatal("analysis is nil")
			}
			if test.name == "named selection" && analysis.OperationName != "B" {
				t.Fatalf("operation name = %q, want B", analysis.OperationName)
			}
		})
	}
}

func TestIntrospectionPolicy(t *testing.T) {
	query := `query Schema { __schema { queryType { name } } }`
	disabled := mustPolicy(t, Options{Limits: testLimits()})
	_, err := disabled.Analyze(Request{Query: query})
	assertPolicyCode(t, err, contracts.ErrorCodeIntrospectionDisabled)

	enabled := mustPolicy(t, Options{
		Limits:             testLimits(),
		AllowIntrospection: true,
	})
	if _, err := enabled.Analyze(Request{Query: query}); err != nil {
		t.Fatal(err)
	}
}

func TestFragmentsExpandDetectCyclesAndChargeWorstCaseDirectives(t *testing.T) {
	checker := mustPolicy(t, Options{Limits: testLimits()})
	analysis, err := checker.Analyze(Request{Query: `
		query Match($include: Boolean!) {
			...MatchRoot @include(if: $include)
		}
		fragment MatchRoot on DotaQuery {
			alias: match(id: 1) {
				... on MatchType { id }
			}
		}
	`, Variables: map[string]any{"include": false}})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Complexity != 2 {
		t.Fatalf("complexity = %d, want 2", analysis.Complexity)
	}
	if len(analysis.RootFields) != 1 || analysis.RootFields[0] != "match" {
		t.Fatalf("roots = %#v", analysis.RootFields)
	}

	_, err = checker.Analyze(Request{Query: `
		query { ...A }
		fragment A on DotaQuery { ...B }
		fragment B on DotaQuery { ...A }
	`})
	assertPolicyCode(t, err, contracts.ErrorCodeInvalidArgument)
}

func TestDocumentAndVariableLimits(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*config.LimitsConfig)
		query     string
		variables map[string]any
		wantCode  contracts.ErrorCode
	}{
		{
			name: "document bytes",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryDocumentBytes = 8
			},
			query:    `query { match(id: 1) { id } }`,
			wantCode: contracts.ErrorCodeQueryDocumentTooLarge,
		},
		{
			name: "variable bytes",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryVariablesBytes = 8
			},
			query:     `query { match(id: 1) { id } }`,
			variables: map[string]any{"value": "long"},
			wantCode:  contracts.ErrorCodeQueryVariablesTooLarge,
		},
		{
			name: "variable depth",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryVariablesDepth = 2
			},
			query:     `query { match(id: 1) { id } }`,
			variables: map[string]any{"a": map[string]any{"b": map[string]any{}}},
			wantCode:  contracts.ErrorCodeQueryVariablesTooLarge,
		},
		{
			name: "variable nodes",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryVariablesNodes = 2
			},
			query:     `query { match(id: 1) { id } }`,
			variables: map[string]any{"a": []any{[]any{}, []any{}}},
			wantCode:  contracts.ErrorCodeQueryVariablesTooLarge,
		},
		{
			name: "variable string",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxIndividualStringSize = 3
			},
			query:     `query { match(id: 1) { id } }`,
			variables: map[string]any{"a": "four"},
			wantCode:  contracts.ErrorCodeQueryVariablesTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := testLimits()
			test.configure(&limits)
			checker := mustPolicy(t, Options{Limits: limits})
			_, err := checker.Analyze(Request{
				Query:     test.query,
				Variables: test.variables,
			})
			assertPolicyCode(t, err, test.wantCode)
		})
	}
}

func TestDepthAliasAndFieldLimits(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*config.LimitsConfig)
		query     string
		wantCode  contracts.ErrorCode
	}{
		{
			name: "depth",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryDepth = 2
			},
			query:    `query { match(id: 1) { players { hero { id } } } }`,
			wantCode: contracts.ErrorCodeQueryDepthExceeded,
		},
		{
			name: "aliases",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryAliases = 1
			},
			query:    `query { one: match(id: 1) { two: id } }`,
			wantCode: contracts.ErrorCodeQueryAliasLimitExceeded,
		},
		{
			name: "expanded fields",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryFields = 2
			},
			query:    `query { match(id: 1) { id durationSeconds } }`,
			wantCode: contracts.ErrorCodeQueryFieldLimitExceeded,
		},
		{
			name: "top-level fields",
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryTopLevelFields = 1
			},
			query:    `query { match(id: 1) { id } team(teamId: 1) { id } }`,
			wantCode: contracts.ErrorCodeQueryFieldLimitExceeded,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := testLimits()
			test.configure(&limits)
			checker := mustPolicy(t, Options{Limits: limits})
			_, err := checker.Analyze(Request{Query: test.query})
			assertPolicyCode(t, err, test.wantCode)
		})
	}
}

func TestListAndComplexityLimits(t *testing.T) {
	listSchema := FieldPolicyFunc(func(path []string, field *ast.Field) FieldRule {
		rule := FieldRule{Kind: FieldObject, Cacheable: true}
		switch field.Name {
		case "items":
			rule.Kind = FieldList
			rule.PageSizeArguments = []string{"first"}
		case "nested":
			rule.Kind = FieldList
			rule.PageSizeArguments = []string{"request.take"}
		case "fixed":
			rule.Kind = FieldList
			rule.FixedMaximum = 3
		default:
			if len(field.SelectionSet) == 0 {
				rule.Kind = FieldScalar
			}
		}
		return rule
	})
	tests := []struct {
		name      string
		query     string
		variables map[string]any
		configure func(*config.LimitsConfig)
		wantCode  contracts.ErrorCode
	}{
		{
			name:     "bound required",
			query:    `query { match(id: 1) { items { id } } }`,
			wantCode: contracts.ErrorCodeQueryListLimitRequired,
		},
		{
			name:     "literal bound exceeded",
			query:    `query { match(id: 1) { items(first: 101) { id } } }`,
			wantCode: contracts.ErrorCodeQueryListLimitExceeded,
		},
		{
			name:      "variable bound exceeded",
			query:     `query Items($count: Int!) { match(id: 1) { items(first: $count) { id } } }`,
			variables: map[string]any{"count": json.Number("101")},
			wantCode:  contracts.ErrorCodeQueryListLimitExceeded,
		},
		{
			name:      "request object bound",
			query:     `query Items($request: Request!) { match(id: 1) { nested(request: $request) { id } } }`,
			variables: map[string]any{"request": map[string]any{"take": json.Number("5")}},
		},
		{
			name:  "complexity",
			query: `query { match(id: 1) { items(first: 5) { id name } } }`,
			configure: func(limits *config.LimitsConfig) {
				limits.MaxQueryComplexity = 10
			},
			wantCode: contracts.ErrorCodeQueryComplexityExceeded,
		},
		{
			name:  "nested list depth",
			query: `query { match(id: 1) { items(first: 2) { nested(request: {take: 2}) { fixed { id } } } } }`,
			configure: func(limits *config.LimitsConfig) {
				limits.MaxNestedListDepth = 2
			},
			wantCode: contracts.ErrorCodeQueryComplexityExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := testLimits()
			if test.configure != nil {
				test.configure(&limits)
			}
			checker := mustPolicy(t, Options{Limits: limits, Schema: listSchema})
			_, err := checker.Analyze(Request{
				Query:     test.query,
				Variables: test.variables,
			})
			assertPolicyCode(t, err, test.wantCode)
		})
	}
}

func TestManifestSchemaPolicyRejectsUnboundedNestedLists(t *testing.T) {
	manifest := schema.Manifest{
		Validation: schema.ValidationMetadata{
			QueryType: "Query",
			Fields: map[string]map[string]schema.Ref{
				"Query": {
					"match": {Type: "Match"},
				},
				"Match": {
					"timeline": {Type: "[TimelineEvent!]!", ListDepth: 1},
				},
				"TimelineEvent": {
					"time": {Type: "Long!"},
				},
			},
		},
	}
	schemaPolicy, err := FromManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	checker := mustPolicy(t, Options{
		Limits: testLimits(),
		Schema: schemaPolicy,
	})
	_, err = checker.Analyze(Request{
		Query: `query { match(id: 1) { timeline { time } } }`,
	})
	assertPolicyCode(t, err, contracts.ErrorCodeQueryListLimitRequired)
}

func TestManifestSchemaPolicyDoesNotTreatFilterListsAsPageBounds(t *testing.T) {
	manifest := schema.Manifest{
		Validation: schema.ValidationMetadata{
			QueryType: "Query",
			Fields: map[string]map[string]schema.Ref{
				"Query": {
					"heroStats": {
						Type:      "[HeroStats!]!",
						ListDepth: 1,
						Arguments: map[string]schema.Ref{
							"heroIds": {Type: "[Long!]!", ListDepth: 1},
						},
					},
				},
				"HeroStats": {"heroId": {Type: "Long!"}},
			},
		},
	}
	schemaPolicy, err := FromManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	checker := mustPolicy(t, Options{Limits: testLimits(), Schema: schemaPolicy})
	_, err = checker.Analyze(Request{
		Query: `query { heroStats(heroIds: [1]) { heroId } }`,
	})
	assertPolicyCode(t, err, contracts.ErrorCodeQueryListLimitRequired)
}

func TestGraphQLSchemaValidationRejectsInvalidDocumentsAndVariables(t *testing.T) {
	graphqlSchema := gqlparser.MustLoadSchema(&ast.Source{Input: `
		type Query { match(id: Int!): Match }
		type Match { id: Int! }
	`})
	checker := mustPolicy(t, Options{
		Limits:        testLimits(),
		GraphQLSchema: graphqlSchema,
	})
	for _, request := range []Request{
		{Query: `query { match(id: 1) { unknown } }`},
		{Query: `query Match($id: Int!) { match(id: $id) { id } }`},
		{
			Query:     `query Match($id: Int!) { match(id: $id) { id } }`,
			Variables: map[string]any{"id": "not-a-number"},
		},
	} {
		_, err := checker.Analyze(request)
		assertPolicyCode(t, err, contracts.ErrorCodeInvalidArgument)
	}
}

func TestDefaultListPolicyAndCacheClassification(t *testing.T) {
	checker := mustPolicy(t, Options{Limits: testLimits()})
	_, err := checker.Analyze(Request{
		Query: `query { leagues(request: {}) { id } }`,
	})
	assertPolicyCode(t, err, contracts.ErrorCodeQueryListLimitRequired)

	analysis, err := checker.Analyze(Request{
		Query: `query Players($ids: [Long]!) {
			players(steamAccountIds: $ids) { identity { name } }
		}`,
		Variables: map[string]any{
			"ids": []any{json.Number("1"), json.Number("2")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Cacheable {
		t.Fatal("sensitive raw query was classified cacheable")
	}
	if len(analysis.SensitiveFields) != 1 ||
		analysis.SensitiveFields[0] != "players.identity" {
		t.Fatalf("sensitive fields = %#v", analysis.SensitiveFields)
	}
}

func TestCanonicalQueryIsStable(t *testing.T) {
	checker := mustPolicy(t, Options{Limits: testLimits()})
	first, err := checker.Analyze(Request{
		Query: `query Match { match(id:1){ id } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := checker.Analyze(Request{
		Query: "query Match {\n  match(id: 1) {\n    id\n  }\n}\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Query != second.Query {
		t.Fatalf("canonical queries differ:\n%s\n---\n%s", first.Query, second.Query)
	}
	if strings.Contains(first.Query, "id:1") {
		t.Fatalf("query was not formatted canonically: %q", first.Query)
	}
}

func testLimits() config.LimitsConfig {
	return config.Defaults(".").Limits
}

func mustPolicy(t *testing.T, options Options) *Policy {
	t.Helper()
	checker, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return checker
}

func assertPolicyCode(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var policyErr *Error
	if !errors.As(err, &policyErr) {
		t.Fatalf("error = %T %v, want policy error %s", err, err, want)
	}
	if policyErr.Code != want {
		t.Fatalf("code = %s, want %s: %v", policyErr.Code, want, policyErr)
	}
}
