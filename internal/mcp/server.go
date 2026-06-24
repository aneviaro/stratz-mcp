package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
	"github.com/aneviaro/stratz-mcp/internal/domain/playermatch"
	rawgraphql "github.com/aneviaro/stratz-mcp/internal/graphql"
	graphqlpolicy "github.com/aneviaro/stratz-mcp/internal/graphql/policy"
	"github.com/aneviaro/stratz-mcp/internal/prompts"
	"github.com/aneviaro/stratz-mcp/internal/resources"
	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

const serverName = "stratz-mcp"

// ToolHandler is the transport-independent tool execution surface used by the
// MCP adapter.
type ToolHandler func(context.Context, any) (any, error)

// Options configures one static MCP server.
type Options struct {
	Version         string
	SchemaVersion   string
	SchemaDirectory string
	CacheStatus     cache.Status
	Cache           *cache.Store
	CacheNamespace  string
	Config          config.Config
	Executor        stratz.Executor
	CursorToken     string
	Logger          *slog.Logger
	Now             func() time.Time
	Handlers        map[string]ToolHandler
}

// Server owns the official SDK server and its immutable runtime dependencies.
type Server struct {
	sdk *sdk.Server
}

// New creates the static v1 MCP surface and registers every generated tool
// schema.
func New(options Options) (*Server, error) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.SchemaVersion == "" {
		options.SchemaVersion = "unavailable"
	}

	handlers := make(map[string]ToolHandler, len(options.Handlers)+1)
	for name, handler := range options.Handlers {
		handlers[name] = handler
	}
	if handlers["stratz_execute_graphql"] == nil {
		manifest, manifestErr := schema.LoadManifest(options.SchemaDirectory)
		if manifestErr != nil {
			handlers["stratz_execute_graphql"] = func(context.Context, any) (any, error) {
				return nil, &ExecutionError{
					Code:      contracts.ErrorCodeQueryOperationNotAllowed,
					Message:   "Raw GraphQL requires local schema metadata; run stratz-mcp schema pull",
					Details:   map[string]any{},
					Retryable: false,
				}
			}
		} else {
			schemaData, schemaErr := os.ReadFile(filepath.Join(
				options.SchemaDirectory,
				schema.FullSchemaFile,
			))
			if schemaErr != nil {
				return nil, fmt.Errorf("read local GraphQL schema: %w", schemaErr)
			}
			graphqlSchema, schemaErr := gqlparser.LoadSchema(&ast.Source{
				Name:  schema.FullSchemaFile,
				Input: string(schemaData),
			})
			if schemaErr != nil {
				return nil, fmt.Errorf("compile local GraphQL schema: %w", schemaErr)
			}
			schemaPolicy, policyErr := graphqlpolicy.FromManifest(manifest)
			if policyErr != nil {
				return nil, policyErr
			}
			rawPolicy, err := graphqlpolicy.New(graphqlpolicy.Options{
				Limits:             options.Config.Limits,
				AllowIntrospection: options.Config.Features.RuntimeIntrospection,
				Schema:             schemaPolicy,
				GraphQLSchema:      graphqlSchema,
			})
			if err != nil {
				return nil, err
			}
			rawService, err := rawgraphql.NewRawService(rawgraphql.RawOptions{
				Policy:              rawPolicy,
				Executor:            options.Executor,
				MaxUpstreamRequests: options.Config.Limits.MaxUpstreamRequests,
				DefaultCacheTTL:     options.Config.Cache.RawTTL,
			})
			if err != nil {
				return nil, err
			}
			handlers["stratz_execute_graphql"] = rawGraphQLHandler(options, rawService)
		}
	}
	heroConstantsService, err := heroconstants.New(heroconstants.Options{
		Executor:            options.Executor,
		MaxUpstreamRequests: options.Config.Limits.MaxUpstreamRequests,
		MaxBatchSize:        options.Config.Limits.MaxBatchSize,
		ConstantsTTL:        options.Config.Cache.PublicReferenceTTL,
		Now:                 options.Now,
	})
	if err != nil {
		return nil, err
	}
	if options.CursorToken != "" {
		playerMatchService, err := playermatch.New(playermatch.Options{
			Executor:            options.Executor,
			Token:               options.CursorToken,
			SchemaVersion:       options.SchemaVersion,
			MaxUpstreamRequests: options.Config.Limits.MaxUpstreamRequests,
			MaxBatchSize:        options.Config.Limits.MaxBatchSize,
			Heroes:              heroConstantsService,
			Now:                 options.Now,
		})
		if err != nil {
			return nil, err
		}
		registerPlayerMatchHandlers(handlers, options, playerMatchService, heroConstantsService)
		leagueLiveService, err := leaguelive.New(leaguelive.Options{
			Executor:            options.Executor,
			Token:               options.CursorToken,
			SchemaVersion:       options.SchemaVersion,
			MaxUpstreamRequests: options.Config.Limits.MaxUpstreamRequests,
			Now:                 options.Now,
		})
		if err != nil {
			return nil, err
		}
		registerLeagueLiveHandlers(handlers, options, leagueLiveService, heroConstantsService)
	}
	registerHeroConstantsHandlers(handlers, options, heroConstantsService)
	handlers["stratz_server_info"] = serverInfoHandler(options)

	server := sdk.NewServer(
		&sdk.Implementation{
			Name:       serverName,
			Title:      "STRATZ MCP",
			Version:    options.Version,
			WebsiteURL: "https://github.com/aneviaro/stratz-mcp",
		},
		&sdk.ServerOptions{
			Instructions: "Unofficial local STRATZ MCP server. Data provided by STRATZ (https://stratz.com).",
			Logger:       options.Logger,
			Capabilities: &sdk.ServerCapabilities{
				Tools:     &sdk.ToolCapabilities{},
				Resources: &sdk.ResourceCapabilities{},
				Prompts:   &sdk.PromptCapabilities{},
			},
		},
	)

	for _, definition := range contracts.Definitions() {
		inputSchema, err := contracts.Schema(definition.Name, contracts.InputSchema)
		if err != nil {
			return nil, err
		}
		outputSchema, err := contracts.Schema(definition.Name, contracts.OutputSchema)
		if err != nil {
			return nil, err
		}
		handler := handlers[definition.Name]
		if specification, ok := cacheSpecifications[definition.Name]; ok {
			handler = cachedToolHandler(options, definition.Name, specification, handler)
		}
		server.AddTool(
			&sdk.Tool{
				Name:         definition.Name,
				Description:  definition.Description,
				InputSchema:  json.RawMessage(inputSchema),
				OutputSchema: json.RawMessage(outputSchema),
			},
			toolAdapter(definition.Name, handler),
		)
	}
	resources.New(options.SchemaDirectory).Register(server)
	prompts.Register(server)
	return &Server{sdk: server}, nil
}

// SDK exposes the configured official SDK server for in-memory conformance
// tests and future transport adapters.
func (server *Server) SDK() *sdk.Server {
	return server.sdk
}

// Run serves one persistent MCP session until EOF, cancellation, or protocol
// failure.
func (server *Server) Run(
	ctx context.Context,
	reader io.ReadCloser,
	writer io.WriteCloser,
) error {
	if reader == nil || writer == nil {
		return errors.New("stdio reader and writer are required")
	}
	return server.sdk.Run(ctx, &sdk.IOTransport{
		Reader: reader,
		Writer: writer,
	})
}

func toolAdapter(name string, handler ToolHandler) sdk.ToolHandler {
	return func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		input, err := decodeArguments(request.Params.Arguments)
		if err != nil {
			return ErrorResult(name, invalidArgumentsError())
		}
		if err := contracts.ValidateInput(name, input); err != nil {
			return ErrorResult(name, invalidArgumentsError())
		}
		if handler == nil {
			return ErrorResult(name, &ExecutionError{
				Code:      contracts.ErrorCodeInternalError,
				Message:   "This tool is registered but is not implemented in the current milestone",
				Details:   map[string]any{"tool": name},
				Retryable: false,
			})
		}

		output, err := handler(ctx, input)
		if err == nil {
			return SuccessResult(name, output)
		}
		var executionErr *ExecutionError
		if errors.As(err, &executionErr) {
			return ErrorResult(name, executionErr)
		}
		return ErrorResult(name, &ExecutionError{
			Code:      contracts.ErrorCodeInternalError,
			Message:   "The tool failed internally",
			Details:   map[string]any{},
			Retryable: false,
		})
	}
}

func invalidArgumentsError() *ExecutionError {
	return &ExecutionError{
		Code:      contracts.ErrorCodeInvalidArgument,
		Message:   "Tool arguments did not match the published input schema",
		Details:   map[string]any{},
		Retryable: false,
	}
}
