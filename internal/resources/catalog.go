package resources

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/securefile"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxResourceBytes = 5 << 20

type Definition struct {
	URI         string
	Name        string
	Title       string
	Description string
	MIMEType    string
	Path        string
}

var definitions = []Definition{
	{URI: "stratz://schema/full", Name: "schema-full", Title: "STRATZ GraphQL schema", Description: "Full locally generated STRATZ GraphQL schema snapshot.", MIMEType: "application/graphql", Path: "schema/full.graphql"},
	{URI: "stratz://schema/player", Name: "schema-player", Title: "STRATZ player schema", Description: "Player-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/player.graphql"},
	{URI: "stratz://schema/match", Name: "schema-match", Title: "STRATZ match schema", Description: "Match-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/match.graphql"},
	{URI: "stratz://schema/hero", Name: "schema-hero", Title: "STRATZ hero schema", Description: "Hero-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/hero.graphql"},
	{URI: "stratz://schema/league", Name: "schema-league", Title: "STRATZ league schema", Description: "League-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/league.graphql"},
	{URI: "stratz://schema/live", Name: "schema-live", Title: "STRATZ live-match schema", Description: "Live-match-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/live.graphql"},
	{URI: "stratz://schema/constants", Name: "schema-constants", Title: "STRATZ constants schema", Description: "Constants-domain STRATZ GraphQL schema subset.", MIMEType: "application/graphql", Path: "schema/constants.graphql"},
	{URI: "stratz://constants/heroes", Name: "constants-heroes", Title: "STRATZ hero constants", Description: "Locally generated STRATZ hero reference constants.", MIMEType: "application/json", Path: "constants/heroes.json"},
	{URI: "stratz://constants/items", Name: "constants-items", Title: "STRATZ item constants", Description: "Locally generated STRATZ item reference constants.", MIMEType: "application/json", Path: "constants/items.json"},
	{URI: "stratz://constants/abilities", Name: "constants-abilities", Title: "STRATZ ability constants", Description: "Locally generated STRATZ ability reference constants.", MIMEType: "application/json", Path: "constants/abilities.json"},
	{URI: "stratz://constants/game-modes", Name: "constants-game-modes", Title: "STRATZ game-mode constants", Description: "Locally generated STRATZ game-mode reference constants.", MIMEType: "application/json", Path: "constants/game-modes.json"},
	{URI: "stratz://constants/regions", Name: "constants-regions", Title: "STRATZ region constants", Description: "Locally generated STRATZ region reference constants.", MIMEType: "application/json", Path: "constants/regions.json"},
	{URI: "stratz://constants/ranks", Name: "constants-ranks", Title: "STRATZ rank constants", Description: "Locally generated rank reference constants when an approved source is available.", MIMEType: "application/json", Path: "constants/ranks.json"},
}

type Catalog struct {
	directory string
}

func New(directory string) *Catalog {
	if strings.TrimSpace(directory) == "" {
		directory = filepath.Join(".", ".stratz-schema-unavailable")
	}
	return &Catalog{directory: filepath.Clean(directory)}
}

func Definitions() []Definition {
	result := make([]Definition, len(definitions))
	copy(result, definitions)
	return result
}

func (catalog *Catalog) Register(server *sdk.Server) {
	for _, definition := range definitions {
		definition := definition
		server.AddResource(&sdk.Resource{
			URI:         definition.URI,
			Name:        definition.Name,
			Title:       definition.Title,
			Description: definition.Description,
			MIMEType:    definition.MIMEType,
		}, catalog.handler(definition))
	}
}

func (catalog *Catalog) handler(definition Definition) sdk.ResourceHandler {
	return func(_ context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		if request == nil || request.Params == nil || request.Params.URI != definition.URI {
			return nil, sdk.ResourceNotFoundError(definition.URI)
		}
		path := filepath.Join(catalog.directory, filepath.FromSlash(definition.Path))
		file, err := securefile.OpenReadOnly(path)
		if err != nil {
			return nil, sdk.ResourceNotFoundError(definition.URI)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, maxResourceBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read resource %s: %w", definition.URI, err)
		}
		if len(data) > maxResourceBytes {
			return nil, fmt.Errorf("resource %s exceeds 5 MiB", definition.URI)
		}
		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{{
				URI:      definition.URI,
				MIMEType: definition.MIMEType,
				Text:     string(data),
			}},
		}, nil
	}
}
