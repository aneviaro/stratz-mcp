package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	ManifestFile      = "manifest.json"
	IntrospectionFile = "introspection.json"
	FullSchemaFile    = "schema/full.graphql"
	MetadataFile      = "validation/metadata.json"
	RestrictionFile   = ".stratz-restricted"
)

var domainRootFields = map[string][]string{
	"player":    {"player", "players"},
	"match":     {"match", "matches"},
	"hero":      {"heroStats"},
	"league":    {"league", "leagues"},
	"live":      {"live"},
	"constants": {"constants"},
}

type Manifest struct {
	FormatVersion int                 `json:"format_version"`
	SchemaHash    string              `json:"schema_hash"`
	Restricted    bool                `json:"restricted"`
	Source        string              `json:"source"`
	Artifacts     map[string]Artifact `json:"artifacts"`
	Domains       map[string]Domain   `json:"domains"`
	Validation    ValidationMetadata  `json:"validation"`
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type Domain struct {
	RootFields []string `json:"root_fields"`
	TypeCount  int      `json:"type_count"`
	SHA256     string   `json:"sha256"`
}

type ValidationMetadata struct {
	QueryType        string                    `json:"query_type"`
	MutationType     string                    `json:"mutation_type,omitempty"`
	SubscriptionType string                    `json:"subscription_type,omitempty"`
	TypeCount        int                       `json:"type_count"`
	DirectiveCount   int                       `json:"directive_count"`
	Fields           map[string]map[string]Ref `json:"fields"`
}

type Ref struct {
	Type      string         `json:"type"`
	ListDepth int            `json:"list_depth"`
	Nullable  bool           `json:"nullable"`
	Arguments map[string]Ref `json:"arguments,omitempty"`
}

type Snapshot struct {
	FormatVersion int      `json:"format_version"`
	Restricted    bool     `json:"restricted"`
	Source        string   `json:"source"`
	Schema        Document `json:"schema"`
}

// Generate normalizes an introspection document and builds the complete local
// schema bundle in memory.
func Generate(document Document) (map[string][]byte, Manifest, error) {
	if err := document.Validate(); err != nil {
		return nil, Manifest{}, err
	}
	document = normalize(document)
	fullSDL, err := formatSDL(document)
	if err != nil {
		return nil, Manifest{}, err
	}
	introspection, err := marshalCanonical(Snapshot{
		FormatVersion: FormatVersion,
		Restricted:    true,
		Source:        "authenticated STRATZ GraphQL introspection",
		Schema:        document,
	})
	if err != nil {
		return nil, Manifest{}, err
	}
	validation := buildValidation(document)
	metadata, err := marshalCanonical(validation)
	if err != nil {
		return nil, Manifest{}, err
	}

	files := map[string][]byte{
		IntrospectionFile: append(introspection, '\n'),
		FullSchemaFile:    fullSDL,
		MetadataFile:      append(metadata, '\n'),
	}
	manifest := Manifest{
		FormatVersion: FormatVersion,
		SchemaHash:    digest(fullSDL),
		Restricted:    true,
		Source:        "authenticated STRATZ GraphQL introspection",
		Artifacts:     map[string]Artifact{},
		Domains:       map[string]Domain{},
		Validation:    validation,
	}

	domainNames := sortedKeys(domainRootFields)
	for _, domainName := range domainNames {
		subset, roots := domainSubset(document, domainRootFields[domainName])
		subsetSDL, err := formatSDL(subset)
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("format %s schema subset: %w", domainName, err)
		}
		path := "schema/" + domainName + ".graphql"
		files[path] = subsetSDL
		manifest.Domains[domainName] = Domain{
			RootFields: roots,
			TypeCount:  len(subset.Types),
			SHA256:     digest(subsetSDL),
		}
	}

	for path, data := range files {
		manifest.Artifacts[path] = Artifact{
			Path:   path,
			SHA256: digest(data),
			Bytes:  len(data),
		}
	}
	manifestJSON, err := marshalCanonical(manifest)
	if err != nil {
		return nil, Manifest{}, err
	}
	files[ManifestFile] = append(manifestJSON, '\n')
	files[RestrictionFile] = []byte(
		"STRATZ_RESTRICTED_UPSTREAM_DATA\n" +
			"Do not commit, package, or publish this directory without approved redistribution clearance.\n",
	)
	return files, manifest, nil
}

// WriteBundle atomically replaces generated files while preserving a clear
// restricted-data marker and owner-only permissions.
func WriteBundle(directory string, files map[string][]byte) (returnErr error) {
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory == "." || directory == "" {
		return errors.New("schema output directory is required")
	}
	if info, err := os.Lstat(directory); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("schema output directory must not be a symlink")
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create schema output parent: %w", err)
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("schema output parent must be a real directory")
	}
	staging, err := os.MkdirTemp(parent, ".schema-bundle-*")
	if err != nil {
		return fmt.Errorf("create schema staging directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(staging); cleanupErr != nil {
			cleanupErr = fmt.Errorf("remove schema staging directory: %w", cleanupErr)
			if returnErr == nil {
				returnErr = cleanupErr
				return
			}
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()
	if err := os.Chmod(staging, 0o700); err != nil {
		return fmt.Errorf("secure schema staging directory: %w", err)
	}
	for _, path := range sortedByteKeys(files) {
		if filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			return fmt.Errorf("unsafe schema artifact path %q", path)
		}
		target := filepath.Join(staging, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create schema artifact directory: %w", err)
		}
		temporary, err := os.CreateTemp(filepath.Dir(target), ".schema-*")
		if err != nil {
			return fmt.Errorf("create temporary schema artifact: %w", err)
		}
		temporaryName := temporary.Name()
		ok := false
		defer func() {
			if !ok {
				if cleanupErr := os.Remove(temporaryName); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
					cleanupErr = fmt.Errorf("remove temporary schema artifact: %w", cleanupErr)
					if returnErr == nil {
						returnErr = cleanupErr
						return
					}
					returnErr = errors.Join(returnErr, cleanupErr)
				}
			}
		}()
		if err := temporary.Chmod(0o600); err != nil {
			return closeTemporaryArtifact(temporary, fmt.Errorf("secure temporary schema artifact: %w", err))
		}
		if _, err := temporary.Write(files[path]); err != nil {
			return closeTemporaryArtifact(temporary, fmt.Errorf("write temporary schema artifact: %w", err))
		}
		if err := temporary.Sync(); err != nil {
			return closeTemporaryArtifact(temporary, fmt.Errorf("sync temporary schema artifact: %w", err))
		}
		if err := temporary.Close(); err != nil {
			return fmt.Errorf("close temporary schema artifact: %w", err)
		}
		if err := os.Rename(temporaryName, target); err != nil {
			return fmt.Errorf("install schema artifact %s: %w", path, err)
		}
		ok = true
	}
	backup := directory + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove previous schema backup: %w", err)
	}
	hadExisting := false
	if _, err := os.Lstat(directory); err == nil {
		if err := os.Rename(directory, backup); err != nil {
			return fmt.Errorf("stage existing schema bundle: %w", err)
		}
		hadExisting = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect existing schema bundle: %w", err)
	}
	if err := os.Rename(staging, directory); err != nil {
		installErr := fmt.Errorf("install schema bundle: %w", err)
		if hadExisting {
			if restoreErr := os.Rename(backup, directory); restoreErr != nil {
				return errors.Join(
					installErr,
					fmt.Errorf("restore previous schema bundle: %w", restoreErr),
				)
			}
		}
		return installErr
	}
	if hadExisting {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove replaced schema bundle: %w", err)
		}
	}
	return nil
}

func closeTemporaryArtifact(file *os.File, operationErr error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(
			operationErr,
			fmt.Errorf("close temporary schema artifact: %w", closeErr),
		)
	}
	return operationErr
}

func normalize(document Document) Document {
	sort.Slice(document.Types, func(i, j int) bool {
		return document.Types[i].Name < document.Types[j].Name
	})
	for index := range document.Types {
		definition := &document.Types[index]
		sort.Slice(definition.Fields, func(i, j int) bool {
			return definition.Fields[i].Name < definition.Fields[j].Name
		})
		for fieldIndex := range definition.Fields {
			sort.Slice(definition.Fields[fieldIndex].Args, func(i, j int) bool {
				return definition.Fields[fieldIndex].Args[i].Name <
					definition.Fields[fieldIndex].Args[j].Name
			})
		}
		sort.Slice(definition.InputFields, func(i, j int) bool {
			return definition.InputFields[i].Name < definition.InputFields[j].Name
		})
		sort.Slice(definition.EnumValues, func(i, j int) bool {
			return definition.EnumValues[i].Name < definition.EnumValues[j].Name
		})
		sort.Slice(definition.Interfaces, func(i, j int) bool {
			return refName(definition.Interfaces[i]) < refName(definition.Interfaces[j])
		})
		sort.Slice(definition.PossibleTypes, func(i, j int) bool {
			return refName(definition.PossibleTypes[i]) < refName(definition.PossibleTypes[j])
		})
	}
	sort.Slice(document.Directives, func(i, j int) bool {
		return document.Directives[i].Name < document.Directives[j].Name
	})
	for index := range document.Directives {
		sort.Strings(document.Directives[index].Locations)
		sort.Slice(document.Directives[index].Args, func(i, j int) bool {
			return document.Directives[index].Args[i].Name <
				document.Directives[index].Args[j].Name
		})
	}
	return document
}

func formatSDL(document Document) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("# Generated from authenticated STRATZ introspection.\n")
	output.WriteString("# Restricted upstream data: do not publish without clearance.\n\n")
	fmt.Fprintf(&output, "schema {\n  query: %s\n", document.QueryType.Name)
	if document.MutationType != nil {
		fmt.Fprintf(&output, "  mutation: %s\n", document.MutationType.Name)
	}
	if document.SubscriptionType != nil {
		fmt.Fprintf(&output, "  subscription: %s\n", document.SubscriptionType.Name)
	}
	output.WriteString("}\n")

	for _, directive := range document.Directives {
		if strings.HasPrefix(directive.Name, "__") ||
			directive.Name == "skip" || directive.Name == "include" ||
			directive.Name == "deprecated" || directive.Name == "specifiedBy" ||
			directive.Name == "oneOf" {
			continue
		}
		output.WriteString("\n")
		writeDescription(&output, directive.Description, "")
		fmt.Fprintf(&output, "directive @%s", directive.Name)
		writeArguments(&output, directive.Args, "")
		if directive.IsRepeatable {
			output.WriteString(" repeatable")
		}
		output.WriteString(" on ")
		output.WriteString(strings.Join(directive.Locations, " | "))
		output.WriteString("\n")
	}

	for _, definition := range document.Types {
		if strings.HasPrefix(definition.Name, "__") || isBuiltinScalar(definition.Name) {
			continue
		}
		output.WriteString("\n")
		writeDescription(&output, definition.Description, "")
		switch definition.Kind {
		case "SCALAR":
			fmt.Fprintf(&output, "scalar %s", definition.Name)
			if definition.SpecifiedBy != nil && *definition.SpecifiedBy != "" {
				fmt.Fprintf(&output, " @specifiedBy(url: %s)", strconv.Quote(*definition.SpecifiedBy))
			}
			output.WriteString("\n")
		case "OBJECT", "INTERFACE":
			keyword := "type"
			if definition.Kind == "INTERFACE" {
				keyword = "interface"
			}
			fmt.Fprintf(&output, "%s %s", keyword, definition.Name)
			if len(definition.Interfaces) > 0 {
				names := make([]string, 0, len(definition.Interfaces))
				for _, ref := range definition.Interfaces {
					names = append(names, refName(ref))
				}
				fmt.Fprintf(&output, " implements %s", strings.Join(names, " & "))
			}
			output.WriteString(" {\n")
			for _, field := range definition.Fields {
				writeDescription(&output, field.Description, "  ")
				fmt.Fprintf(&output, "  %s", field.Name)
				writeArguments(&output, field.Args, "  ")
				fmt.Fprintf(&output, ": %s", formatRef(field.Type))
				writeDeprecated(&output, field.IsDeprecated, field.DeprecationReason)
				output.WriteString("\n")
			}
			output.WriteString("}\n")
		case "INPUT_OBJECT":
			fmt.Fprintf(&output, "input %s {\n", definition.Name)
			for _, field := range definition.InputFields {
				writeDescription(&output, field.Description, "  ")
				fmt.Fprintf(&output, "  %s: %s", field.Name, formatRef(field.Type))
				if field.DefaultValue != nil {
					fmt.Fprintf(&output, " = %s", *field.DefaultValue)
				}
				writeDeprecated(&output, field.IsDeprecated, field.DeprecationReason)
				output.WriteString("\n")
			}
			output.WriteString("}\n")
		case "ENUM":
			fmt.Fprintf(&output, "enum %s {\n", definition.Name)
			for _, value := range definition.EnumValues {
				writeDescription(&output, value.Description, "  ")
				fmt.Fprintf(&output, "  %s", value.Name)
				writeDeprecated(&output, value.IsDeprecated, value.DeprecationReason)
				output.WriteString("\n")
			}
			output.WriteString("}\n")
		case "UNION":
			names := make([]string, 0, len(definition.PossibleTypes))
			for _, possible := range definition.PossibleTypes {
				names = append(names, refName(possible))
			}
			fmt.Fprintf(&output, "union %s = %s\n", definition.Name, strings.Join(names, " | "))
		default:
			return nil, fmt.Errorf("unsupported introspection type kind %q", definition.Kind)
		}
	}
	return output.Bytes(), nil
}

func writeArguments(output *bytes.Buffer, values []InputValue, _ string) {
	if len(values) == 0 {
		return
	}
	output.WriteString("(")
	for index, value := range values {
		if index > 0 {
			output.WriteString(", ")
		}
		fmt.Fprintf(output, "%s: %s", value.Name, formatRef(value.Type))
		if value.DefaultValue != nil {
			fmt.Fprintf(output, " = %s", *value.DefaultValue)
		}
	}
	output.WriteString(")")
}

func writeDescription(output *bytes.Buffer, description *string, indent string) {
	if description == nil || strings.TrimSpace(*description) == "" {
		return
	}
	fmt.Fprintf(output, "%s%s\n", indent, strconv.Quote(*description))
}

func writeDeprecated(output *bytes.Buffer, deprecated bool, reason *string) {
	if !deprecated {
		return
	}
	if reason == nil || *reason == "" || *reason == "No longer supported" {
		output.WriteString(" @deprecated")
		return
	}
	fmt.Fprintf(output, " @deprecated(reason: %s)", strconv.Quote(*reason))
}

func formatRef(ref TypeRef) string {
	switch ref.Kind {
	case "NON_NULL":
		if ref.OfType == nil {
			return "Unknown!"
		}
		return formatRef(*ref.OfType) + "!"
	case "LIST":
		if ref.OfType == nil {
			return "[Unknown]"
		}
		return "[" + formatRef(*ref.OfType) + "]"
	default:
		if ref.Name == nil || *ref.Name == "" {
			return "Unknown"
		}
		return *ref.Name
	}
}

func buildValidation(document Document) ValidationMetadata {
	metadata := ValidationMetadata{
		QueryType:      document.QueryType.Name,
		TypeCount:      len(document.Types),
		DirectiveCount: len(document.Directives),
		Fields:         map[string]map[string]Ref{},
	}
	if document.MutationType != nil {
		metadata.MutationType = document.MutationType.Name
	}
	if document.SubscriptionType != nil {
		metadata.SubscriptionType = document.SubscriptionType.Name
	}
	for _, definition := range document.Types {
		if len(definition.Fields) == 0 && len(definition.InputFields) == 0 {
			continue
		}
		fields := map[string]Ref{}
		for _, field := range definition.Fields {
			ref := Ref{
				Type:      formatRef(field.Type),
				ListDepth: listDepth(field.Type),
				Nullable:  field.Type.Kind != "NON_NULL",
			}
			if len(field.Args) > 0 {
				ref.Arguments = make(map[string]Ref, len(field.Args))
				for _, argument := range field.Args {
					ref.Arguments[argument.Name] = Ref{
						Type:      formatRef(argument.Type),
						ListDepth: listDepth(argument.Type),
						Nullable:  argument.Type.Kind != "NON_NULL",
					}
				}
			}
			fields[field.Name] = ref
		}
		for _, field := range definition.InputFields {
			fields[field.Name] = Ref{
				Type:      formatRef(field.Type),
				ListDepth: listDepth(field.Type),
				Nullable:  field.Type.Kind != "NON_NULL",
			}
		}
		metadata.Fields[definition.Name] = fields
	}
	return metadata
}

func domainSubset(document Document, allowed []string) (Document, []string) {
	byName := make(map[string]Type, len(document.Types))
	for _, definition := range document.Types {
		byName[definition.Name] = definition
	}
	root := byName[document.QueryType.Name]
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}
	selectedRoots := make([]string, 0, len(allowed))
	filteredFields := make([]Field, 0, len(root.Fields))
	required := map[string]struct{}{root.Name: {}}
	for _, field := range root.Fields {
		if _, ok := allowedSet[field.Name]; !ok {
			continue
		}
		filteredFields = append(filteredFields, field)
		selectedRoots = append(selectedRoots, field.Name)
		collectFieldRefs(field, required)
	}
	root.Fields = filteredFields
	byName[root.Name] = root

	queue := make([]string, 0, len(required))
	enqueued := map[string]struct{}{}
	for _, name := range sortedKeys(required) {
		queue = append(queue, name)
		enqueued[name] = struct{}{}
	}
	processed := map[string]struct{}{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		delete(enqueued, name)
		if _, done := processed[name]; done {
			continue
		}
		processed[name] = struct{}{}
		definition, ok := byName[name]
		if !ok {
			continue
		}
		collectTypeRefs(definition, required)
		for _, candidate := range sortedKeys(required) {
			if _, done := processed[candidate]; done {
				continue
			}
			if _, pending := enqueued[candidate]; pending {
				continue
			}
			queue = append(queue, candidate)
			enqueued[candidate] = struct{}{}
		}
	}
	subset := document
	subset.MutationType = nil
	subset.SubscriptionType = nil
	subset.Directives = nil
	subset.Types = nil
	for _, definition := range document.Types {
		if _, ok := required[definition.Name]; ok || isBuiltinScalar(definition.Name) {
			if definition.Name == root.Name {
				definition = root
			}
			subset.Types = append(subset.Types, definition)
		}
	}
	sort.Strings(selectedRoots)
	return normalize(subset), selectedRoots
}

func collectTypeRefs(definition Type, required map[string]struct{}) {
	for _, field := range definition.Fields {
		collectFieldRefs(field, required)
	}
	for _, field := range definition.InputFields {
		collectRef(field.Type, required)
	}
	for _, ref := range definition.Interfaces {
		collectRef(ref, required)
	}
	for _, ref := range definition.PossibleTypes {
		collectRef(ref, required)
	}
}

func collectFieldRefs(field Field, required map[string]struct{}) {
	collectRef(field.Type, required)
	for _, argument := range field.Args {
		collectRef(argument.Type, required)
	}
}

func collectRef(ref TypeRef, required map[string]struct{}) {
	for current := &ref; current != nil; current = current.OfType {
		if current.Name != nil && *current.Name != "" {
			required[*current.Name] = struct{}{}
		}
	}
}

func listDepth(ref TypeRef) int {
	depth := 0
	for current := &ref; current != nil; current = current.OfType {
		if current.Kind == "LIST" {
			depth++
		}
	}
	return depth
}

func isBuiltinScalar(name string) bool {
	switch name {
	case "String", "Boolean", "Int", "Float", "ID":
		return true
	default:
		return false
	}
}

func refName(ref TypeRef) string {
	for current := &ref; current != nil; current = current.OfType {
		if current.Name != nil {
			return *current.Name
		}
	}
	return ""
}

func marshalCanonical(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return json.MarshalIndent(normalized, "", "  ")
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedByteKeys(values map[string][]byte) []string {
	return sortedKeys(values)
}
