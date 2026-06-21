package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/parser"
)

var allowedRoots = stringSet(contracts.RawGraphQLAllowedRootFields())
var deniedRoots = stringSet(contracts.RawGraphQLDeniedRootFields())

// Error is a stable raw-query policy failure.
type Error struct {
	Code    contracts.ErrorCode
	Message string
	Details map[string]any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// Request is the caller-controlled raw GraphQL input.
type Request struct {
	Query         string
	Variables     map[string]any
	OperationName string
	Cache         bool
}

// Analysis is the validated, canonical request plus policy facts used by
// execution and future cache implementations.
type Analysis struct {
	Query           string
	Variables       map[string]any
	OperationName   string
	RootFields      []string
	SelectedFields  []string
	Complexity      int
	Cacheable       bool
	SensitiveFields []string
}

// FieldKind describes the response shape needed for deterministic demand
// control. Task 8 can populate these rules from generated schema metadata.
type FieldKind uint8

const (
	FieldUnknown FieldKind = iota
	FieldScalar
	FieldObject
	FieldList
)

// FieldRule is schema-derived demand and data-classification metadata.
type FieldRule struct {
	Kind              FieldKind
	PageSizeArguments []string
	FixedMaximum      int
	Cacheable         bool
	Sensitive         bool
}

// SchemaPolicy supplies field metadata without coupling validation to a
// redistributed upstream schema snapshot.
type SchemaPolicy interface {
	Field(path []string, field *ast.Field) FieldRule
}

// FieldPolicyFunc adapts a function into a schema policy.
type FieldPolicyFunc func([]string, *ast.Field) FieldRule

func (function FieldPolicyFunc) Field(path []string, field *ast.Field) FieldRule {
	return function(path, field)
}

// Options configures immutable raw-query policy.
type Options struct {
	Limits             config.LimitsConfig
	AllowIntrospection bool
	Schema             SchemaPolicy
}

// Policy validates raw GraphQL requests.
type Policy struct {
	limits             config.LimitsConfig
	allowIntrospection bool
	schema             SchemaPolicy
}

// New creates a bounded raw-query policy.
func New(options Options) (*Policy, error) {
	defaults := config.Defaults(".").Limits
	limits := options.Limits
	if limits.MaxQueryDocumentBytes == 0 {
		limits = defaults
	}
	if limits.MaxQueryDocumentBytes < 1 ||
		limits.MaxQueryVariablesBytes < 1 ||
		limits.MaxQueryVariablesDepth < 1 ||
		limits.MaxQueryVariablesNodes < 1 ||
		limits.MaxQueryDepth < 1 ||
		limits.MaxQueryAliases < 1 ||
		limits.MaxQueryFields < 1 ||
		limits.MaxQueryTopLevelFields < 1 ||
		limits.MaxQueryComplexity < 1 ||
		limits.MaxListPageSize < 1 ||
		limits.MaxNestedListDepth < 1 ||
		limits.MaxGraphQLOperations < 1 ||
		limits.MaxIndividualStringSize < 1 {
		return nil, fmt.Errorf("raw GraphQL limits must be positive")
	}
	schema := options.Schema
	if schema == nil {
		schema = defaultSchemaPolicy{}
	}
	return &Policy{
		limits:             limits,
		allowIntrospection: options.AllowIntrospection,
		schema:             schema,
	}, nil
}

// Analyze parses, selects, expands, and bounds one raw operation.
func (policy *Policy) Analyze(request Request) (*Analysis, error) {
	if int64(len(request.Query)) > policy.limits.MaxQueryDocumentBytes {
		return nil, policyError(
			contracts.ErrorCodeQueryDocumentTooLarge,
			"GraphQL document exceeds the configured size limit",
			"limit", policy.limits.MaxQueryDocumentBytes,
			"actual", len(request.Query),
		)
	}
	if strings.TrimSpace(request.Query) == "" {
		return nil, invalid("GraphQL query is required")
	}
	variables := request.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	if err := policy.validateVariables(variables); err != nil {
		return nil, err
	}

	tokenLimit := int(policy.limits.MaxQueryDocumentBytes/2) + 1
	document, parseErr := parser.ParseQueryWithTokenLimit(
		&ast.Source{Name: "raw.graphql", Input: request.Query},
		tokenLimit,
	)
	if parseErr != nil {
		return nil, invalid("GraphQL document is not syntactically valid")
	}
	if len(document.Operations) > policy.limits.MaxGraphQLOperations {
		return nil, policyError(
			contracts.ErrorCodeQueryOperationLimitExceeded,
			"GraphQL document contains too many operations",
			"limit", policy.limits.MaxGraphQLOperations,
			"actual", len(document.Operations),
		)
	}
	operation, err := selectOperation(document, request.OperationName)
	if err != nil {
		return nil, err
	}
	if operation.Operation != ast.Query {
		return nil, policyError(
			contracts.ErrorCodeQueryOperationNotAllowed,
			"Only GraphQL query operations are allowed",
			"operation_type", operation.Operation,
		)
	}

	analyzer := queryAnalyzer{
		policy:     policy,
		document:   document,
		variables:  variables,
		seenRoots:  make(map[string]struct{}),
		seenFields: make(map[string]struct{}),
		cacheable:  true,
	}
	complexity, walkErr := analyzer.walk(operation.SelectionSet, nil, 1, 0, nil)
	if walkErr != nil {
		return nil, walkErr
	}
	if analyzer.topLevelFields > policy.limits.MaxQueryTopLevelFields {
		return nil, policyError(
			contracts.ErrorCodeQueryFieldLimitExceeded,
			"GraphQL operation selects too many top-level fields",
			"limit", policy.limits.MaxQueryTopLevelFields,
			"actual", analyzer.topLevelFields,
			"scope", "top_level",
		)
	}
	if analyzer.fields > policy.limits.MaxQueryFields {
		return nil, policyError(
			contracts.ErrorCodeQueryFieldLimitExceeded,
			"GraphQL operation selects too many fields after fragment expansion",
			"limit", policy.limits.MaxQueryFields,
			"actual", analyzer.fields,
			"scope", "expanded",
		)
	}
	if analyzer.aliases > policy.limits.MaxQueryAliases {
		return nil, policyError(
			contracts.ErrorCodeQueryAliasLimitExceeded,
			"GraphQL operation uses too many aliases",
			"limit", policy.limits.MaxQueryAliases,
			"actual", analyzer.aliases,
		)
	}
	if analyzer.depth > policy.limits.MaxQueryDepth {
		return nil, policyError(
			contracts.ErrorCodeQueryDepthExceeded,
			"GraphQL operation exceeds the configured depth limit",
			"limit", policy.limits.MaxQueryDepth,
			"actual", analyzer.depth,
		)
	}
	if complexity > policy.limits.MaxQueryComplexity {
		return nil, policyError(
			contracts.ErrorCodeQueryComplexityExceeded,
			"GraphQL operation exceeds the configured complexity limit",
			"limit", policy.limits.MaxQueryComplexity,
			"actual", complexity,
		)
	}

	var canonical bytes.Buffer
	formatter.NewFormatter(&canonical).FormatQueryDocument(document)
	return &Analysis{
		Query:           strings.TrimSpace(canonical.String()),
		Variables:       variables,
		OperationName:   operation.Name,
		RootFields:      analyzer.roots,
		SelectedFields:  analyzer.selected,
		Complexity:      complexity,
		Cacheable:       analyzer.cacheable && len(analyzer.sensitive) == 0,
		SensitiveFields: analyzer.sensitive,
	}, nil
}

func selectOperation(document *ast.QueryDocument, name string) (*ast.OperationDefinition, error) {
	if len(document.Operations) == 0 {
		return nil, invalid("GraphQL document does not contain an operation")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if len(document.Operations) != 1 {
			return nil, invalid("operation_name is required for an ambiguous GraphQL document")
		}
		return document.Operations[0], nil
	}
	operation := document.Operations.ForName(name)
	if operation == nil {
		return nil, invalid("operation_name does not identify an operation in the document")
	}
	return operation, nil
}

func (policy *Policy) validateVariables(variables map[string]any) error {
	encoded, err := json.Marshal(variables)
	if err != nil {
		return invalid("GraphQL variables are not JSON-compatible")
	}
	if int64(len(encoded)) > policy.limits.MaxQueryVariablesBytes {
		return policyError(
			contracts.ErrorCodeQueryVariablesTooLarge,
			"GraphQL variables exceed the configured size limit",
			"limit", policy.limits.MaxQueryVariablesBytes,
			"actual", len(encoded),
		)
	}
	nodes := 0
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > policy.limits.MaxQueryVariablesDepth {
			return policyError(
				contracts.ErrorCodeQueryVariablesTooLarge,
				"GraphQL variables exceed the configured nesting limit",
				"limit", policy.limits.MaxQueryVariablesDepth,
				"actual", depth,
				"constraint", "depth",
			)
		}
		switch typed := value.(type) {
		case map[string]any:
			nodes++
			if nodes > policy.limits.MaxQueryVariablesNodes {
				return policyError(
					contracts.ErrorCodeQueryVariablesTooLarge,
					"GraphQL variables contain too many object and array nodes",
					"limit", policy.limits.MaxQueryVariablesNodes,
					"actual", nodes,
					"constraint", "nodes",
				)
			}
			for _, child := range typed {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			nodes++
			if nodes > policy.limits.MaxQueryVariablesNodes {
				return policyError(
					contracts.ErrorCodeQueryVariablesTooLarge,
					"GraphQL variables contain too many object and array nodes",
					"limit", policy.limits.MaxQueryVariablesNodes,
					"actual", nodes,
					"constraint", "nodes",
				)
			}
			for _, child := range typed {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case string:
			if int64(len(typed)) > policy.limits.MaxIndividualStringSize {
				return policyError(
					contracts.ErrorCodeQueryVariablesTooLarge,
					"GraphQL variable string exceeds the configured size limit",
					"limit", policy.limits.MaxIndividualStringSize,
					"actual", len(typed),
					"constraint", "string",
				)
			}
		}
		return nil
	}
	return visit(variables, 1)
}

type queryAnalyzer struct {
	policy         *Policy
	document       *ast.QueryDocument
	variables      map[string]any
	fragmentStack  []string
	fields         int
	topLevelFields int
	aliases        int
	depth          int
	roots          []string
	selected       []string
	sensitive      []string
	seenRoots      map[string]struct{}
	seenFields     map[string]struct{}
	cacheable      bool
}

func (analyzer *queryAnalyzer) walk(
	selections ast.SelectionSet,
	path []string,
	depth int,
	listDepth int,
	fragmentStack []string,
) (int, error) {
	total := 0
	for _, selection := range selections {
		switch typed := selection.(type) {
		case *ast.Field:
			fieldPath := appendPath(path, typed.Name)
			analyzer.fields++
			if analyzer.fields > analyzer.policy.limits.MaxQueryFields {
				return 0, policyError(
					contracts.ErrorCodeQueryFieldLimitExceeded,
					"GraphQL operation selects too many fields after fragment expansion",
					"limit", analyzer.policy.limits.MaxQueryFields,
					"actual", analyzer.fields,
					"scope", "expanded",
				)
			}
			if len(path) == 0 {
				analyzer.topLevelFields++
				if analyzer.topLevelFields > analyzer.policy.limits.MaxQueryTopLevelFields {
					return 0, policyError(
						contracts.ErrorCodeQueryFieldLimitExceeded,
						"GraphQL operation selects too many top-level fields",
						"limit", analyzer.policy.limits.MaxQueryTopLevelFields,
						"actual", analyzer.topLevelFields,
						"scope", "top_level",
					)
				}
				if err := analyzer.validateRoot(typed.Name); err != nil {
					return 0, err
				}
				if _, exists := analyzer.seenRoots[typed.Name]; !exists {
					analyzer.seenRoots[typed.Name] = struct{}{}
					analyzer.roots = append(analyzer.roots, typed.Name)
				}
			}
			if typed.Alias != "" && typed.Alias != typed.Name {
				analyzer.aliases++
				if analyzer.aliases > analyzer.policy.limits.MaxQueryAliases {
					return 0, policyError(
						contracts.ErrorCodeQueryAliasLimitExceeded,
						"GraphQL operation uses too many aliases",
						"limit", analyzer.policy.limits.MaxQueryAliases,
						"actual", analyzer.aliases,
					)
				}
			}
			if depth > analyzer.depth {
				analyzer.depth = depth
			}
			if depth > analyzer.policy.limits.MaxQueryDepth {
				return 0, policyError(
					contracts.ErrorCodeQueryDepthExceeded,
					"GraphQL operation exceeds the configured depth limit",
					"limit", analyzer.policy.limits.MaxQueryDepth,
					"actual", depth,
				)
			}
			if strings.HasPrefix(typed.Name, "__") {
				if err := analyzer.validateMetaField(typed.Name); err != nil {
					return 0, err
				}
			}
			pathText := strings.Join(fieldPath, ".")
			if _, exists := analyzer.seenFields[pathText]; !exists {
				analyzer.seenFields[pathText] = struct{}{}
				analyzer.selected = append(analyzer.selected, pathText)
			}

			rule := analyzer.policy.schema.Field(fieldPath, typed)
			if rule.Sensitive {
				analyzer.sensitive = appendUnique(analyzer.sensitive, pathText)
			}
			if !rule.Cacheable {
				analyzer.cacheable = false
			}
			childCost := 0
			if len(typed.SelectionSet) > 0 {
				nextListDepth := listDepth
				if rule.Kind == FieldList {
					nextListDepth++
					if nextListDepth > analyzer.policy.limits.MaxNestedListDepth {
						return 0, policyError(
							contracts.ErrorCodeQueryComplexityExceeded,
							"GraphQL operation exceeds the nested-list limit",
							"limit", analyzer.policy.limits.MaxNestedListDepth,
							"actual", nextListDepth,
							"path", pathText,
						)
					}
				}
				var err error
				childCost, err = analyzer.walk(
					typed.SelectionSet,
					fieldPath,
					depth+1,
					nextListDepth,
					fragmentStack,
				)
				if err != nil {
					return 0, err
				}
			}
			fieldCost := 1 + childCost
			if rule.Kind == FieldList {
				pageSize, err := analyzer.pageSize(typed, rule, pathText)
				if err != nil {
					return 0, err
				}
				fieldCost = 1 + pageSize*max(1, childCost)
			}
			total += fieldCost
			if total > analyzer.policy.limits.MaxQueryComplexity {
				return total, policyError(
					contracts.ErrorCodeQueryComplexityExceeded,
					"GraphQL operation exceeds the configured complexity limit",
					"limit", analyzer.policy.limits.MaxQueryComplexity,
					"actual", total,
				)
			}
		case *ast.InlineFragment:
			cost, err := analyzer.walk(
				typed.SelectionSet,
				path,
				depth+1,
				listDepth,
				fragmentStack,
			)
			if err != nil {
				return 0, err
			}
			total += cost
		case *ast.FragmentSpread:
			if contains(fragmentStack, typed.Name) {
				cycle := append(append([]string(nil), fragmentStack...), typed.Name)
				return 0, invalidWithDetails(
					"GraphQL fragment cycle detected",
					map[string]any{"cycle": cycle},
				)
			}
			fragment := analyzer.document.Fragments.ForName(typed.Name)
			if fragment == nil {
				return 0, invalidWithDetails(
					"GraphQL fragment spread references an unknown fragment",
					map[string]any{"fragment": typed.Name},
				)
			}
			if len(fragmentStack)+1 > analyzer.policy.limits.MaxQueryFields {
				return 0, policyError(
					contracts.ErrorCodeQueryFieldLimitExceeded,
					"GraphQL fragment expansion exceeds the configured limit",
					"limit", analyzer.policy.limits.MaxQueryFields,
					"actual", len(fragmentStack)+1,
					"scope", "fragment_chain",
				)
			}
			cost, err := analyzer.walk(
				fragment.SelectionSet,
				path,
				depth,
				listDepth,
				append(fragmentStack, typed.Name),
			)
			if err != nil {
				return 0, err
			}
			total += cost
		default:
			return 0, invalid("GraphQL document contains an unsupported selection")
		}
	}
	return total, nil
}

func (analyzer *queryAnalyzer) validateRoot(name string) error {
	if name == "__typename" {
		return nil
	}
	if name == "__schema" || name == "__type" {
		if analyzer.policy.allowIntrospection {
			return nil
		}
		return policyError(
			contracts.ErrorCodeIntrospectionDisabled,
			"Runtime GraphQL introspection is disabled",
			"field", name,
		)
	}
	if _, denied := deniedRoots[name]; denied {
		return policyError(
			contracts.ErrorCodeQueryOperationNotAllowed,
			"GraphQL root field is explicitly denied",
			"root_field", name,
		)
	}
	if _, allowed := allowedRoots[name]; !allowed {
		return policyError(
			contracts.ErrorCodeQueryOperationNotAllowed,
			"GraphQL root field is not approved by the default-deny policy",
			"root_field", name,
		)
	}
	return nil
}

func (analyzer *queryAnalyzer) validateMetaField(name string) error {
	switch name {
	case "__typename":
		return nil
	case "__schema", "__type":
		if analyzer.policy.allowIntrospection {
			return nil
		}
		return policyError(
			contracts.ErrorCodeIntrospectionDisabled,
			"Runtime GraphQL introspection is disabled",
			"field", name,
		)
	default:
		return policyError(
			contracts.ErrorCodeQueryOperationNotAllowed,
			"GraphQL meta field is not approved",
			"field", name,
		)
	}
}

func (analyzer *queryAnalyzer) pageSize(
	field *ast.Field,
	rule FieldRule,
	path string,
) (int, error) {
	if rule.FixedMaximum > 0 {
		if rule.FixedMaximum > analyzer.policy.limits.MaxListPageSize {
			return 0, policyError(
				contracts.ErrorCodeQueryListLimitExceeded,
				"GraphQL fixed-size list exceeds the configured page limit",
				"limit", analyzer.policy.limits.MaxListPageSize,
				"actual", rule.FixedMaximum,
				"path", path,
			)
		}
		return rule.FixedMaximum, nil
	}
	for _, argumentPath := range rule.PageSizeArguments {
		value, found, err := argumentValue(field.Arguments, strings.Split(argumentPath, "."), analyzer.variables)
		if err != nil {
			return 0, invalidWithDetails(
				"GraphQL list bound is not a valid integer",
				map[string]any{"path": path, "argument": argumentPath},
			)
		}
		if !found {
			continue
		}
		size, ok := integerSize(value)
		if !ok || size < 1 {
			return 0, invalidWithDetails(
				"GraphQL list bound must be a positive integer or non-empty ID list",
				map[string]any{"path": path, "argument": argumentPath},
			)
		}
		if size > analyzer.policy.limits.MaxListPageSize {
			return 0, policyError(
				contracts.ErrorCodeQueryListLimitExceeded,
				"GraphQL list bound exceeds the configured page limit",
				"limit", analyzer.policy.limits.MaxListPageSize,
				"actual", size,
				"path", path,
				"argument", argumentPath,
			)
		}
		return size, nil
	}
	return 0, policyError(
		contracts.ErrorCodeQueryListLimitRequired,
		"GraphQL list field requires a static or variable-supplied bound",
		"path", path,
		"accepted_arguments", rule.PageSizeArguments,
	)
}

func argumentValue(
	arguments ast.ArgumentList,
	path []string,
	variables map[string]any,
) (any, bool, error) {
	if len(path) == 0 {
		return nil, false, nil
	}
	argument := arguments.ForName(path[0])
	if argument == nil {
		return nil, false, nil
	}
	value, err := valueOf(argument.Value, variables)
	if err != nil {
		return nil, true, err
	}
	for _, component := range path[1:] {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		value, ok = object[component]
		if !ok {
			return nil, false, nil
		}
	}
	return value, true, nil
}

func valueOf(value *ast.Value, variables map[string]any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if value.Kind == ast.Variable {
		result, exists := variables[value.Raw]
		if !exists {
			return nil, nil
		}
		return result, nil
	}
	switch value.Kind {
	case ast.IntValue:
		var number json.Number = json.Number(value.Raw)
		return number, nil
	case ast.ListValue:
		result := make([]any, 0, len(value.Children))
		for _, child := range value.Children {
			item, err := valueOf(child.Value, variables)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, nil
	case ast.ObjectValue:
		result := make(map[string]any, len(value.Children))
		for _, child := range value.Children {
			item, err := valueOf(child.Value, variables)
			if err != nil {
				return nil, err
			}
			result[child.Name] = item
		}
		return result, nil
	default:
		return value.Value(variables)
	}
}

func integerSize(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		return int(number), err == nil && number <= int64(^uint(0)>>1)
	case int:
		return typed, true
	case int64:
		return int(typed), int64(int(typed)) == typed
	case float64:
		integer := int(typed)
		return integer, float64(integer) == typed
	case []any:
		return len(typed), true
	}
	valueOf := reflect.ValueOf(value)
	if valueOf.IsValid() && (valueOf.Kind() == reflect.Array || valueOf.Kind() == reflect.Slice) {
		return valueOf.Len(), true
	}
	return 0, false
}

type defaultSchemaPolicy struct{}

func (defaultSchemaPolicy) Field(path []string, field *ast.Field) FieldRule {
	rule := FieldRule{Kind: FieldUnknown, Cacheable: true}
	pathText := strings.Join(path, ".")
	switch pathText {
	case "leagues":
		rule.Kind = FieldList
		rule.PageSizeArguments = []string{"request.take"}
	case "matches":
		rule.Kind = FieldList
		rule.PageSizeArguments = []string{"ids"}
	case "players":
		rule.Kind = FieldList
		rule.PageSizeArguments = []string{"steamAccountIds"}
	case "teams":
		rule.Kind = FieldList
		rule.PageSizeArguments = []string{"teamIds"}
	case "live.matches":
		rule.Kind = FieldList
		rule.PageSizeArguments = []string{"request.take"}
	case "match.players", "live.matches.players", "match.fights.participants":
		rule.Kind = FieldList
		rule.FixedMaximum = 10
	case "match.timeline", "match.economy":
		rule.Kind = FieldList
		rule.FixedMaximum = 5000
	case "match.objectives", "match.fights", "constants.heroes",
		"constants.items", "constants.abilities", "constants.gameModes",
		"constants.regions", "constants.ranks", "heroStats.matchups",
		"heroStats.synergies":
		rule.Kind = FieldList
		rule.FixedMaximum = 500
	default:
		if len(path) > 1 && field.Name == "matches" {
			rule.Kind = FieldList
			rule.PageSizeArguments = []string{"request.take"}
		} else if len(field.SelectionSet) > 0 {
			rule.Kind = FieldObject
		} else {
			rule.Kind = FieldScalar
		}
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

func appendPath(path []string, value string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = value
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func invalid(message string) *Error {
	return invalidWithDetails(message, map[string]any{})
}

func invalidWithDetails(message string, details map[string]any) *Error {
	return &Error{
		Code:    contracts.ErrorCodeInvalidArgument,
		Message: message,
		Details: details,
	}
}

func policyError(
	code contracts.ErrorCode,
	message string,
	details ...any,
) *Error {
	safeDetails := make(map[string]any, len(details)/2)
	for index := 0; index+1 < len(details); index += 2 {
		key, ok := details[index].(string)
		if ok {
			safeDetails[key] = details[index+1]
		}
	}
	return &Error{Code: code, Message: message, Details: safeDetails}
}
