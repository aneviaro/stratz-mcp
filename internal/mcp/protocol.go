package mcp

import (
	"context"
	"io"
	"slices"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	legacyProtocolVersion     = "2025-11-25"
	protocolDiscoveryCacheTTL = 3_600_000
	protocolCatalogCacheTTL   = 300_000
)

type stdioTransport struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func newStdioTransport(reader io.ReadCloser, writer io.WriteCloser) *stdioTransport {
	return &stdioTransport{reader: reader, writer: writer}
}

func (transport *stdioTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	return (&sdk.IOTransport{
		Reader: transport.reader,
		Writer: transport.writer,
	}).Connect(ctx)
}

func (*stdioTransport) SupportsProtocolVersion(version string) bool {
	return slices.Contains(contracts.SupportedMCPProtocolVersions(), version)
}

// protocolMiddleware enforces request-level protocol state and applies cache
// hints to SDK results without coupling the hints to transport method names.
func protocolMiddleware(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
		if request != nil {
			if session, ok := request.GetSession().(*sdk.ServerSession); ok {
				initialized := session.InitializeParams()
				if initialized != nil && initialized.ProtocolVersion == contracts.MCPProtocolVersion {
					params := request.GetParams()
					if params == nil {
						return nil, modernProtocolMetadataError()
					}
					version, ok := params.GetMeta()[sdk.MetaKeyProtocolVersion].(string)
					if !ok || version != initialized.ProtocolVersion {
						return nil, modernProtocolMetadataError()
					}
				}
			}
		}

		result, err := next(ctx, method, request)
		if err != nil {
			return result, err
		}
		switch result := result.(type) {
		case *sdk.DiscoverResult:
			result.TTLMs = protocolDiscoveryCacheTTL
			result.CacheScope = "public"
		case *sdk.ListToolsResult:
			result.TTLMs = protocolCatalogCacheTTL
			result.CacheScope = "public"
		case *sdk.ListPromptsResult:
			result.TTLMs = protocolCatalogCacheTTL
			result.CacheScope = "public"
		case *sdk.ListResourcesResult:
			result.TTLMs = protocolCatalogCacheTTL
			result.CacheScope = "public"
		case *sdk.ListResourceTemplatesResult:
			result.TTLMs = protocolCatalogCacheTTL
			result.CacheScope = "public"
		case *sdk.ReadResourceResult:
			result.TTLMs = 0
			result.CacheScope = "private"
		}
		return result, nil
	}
}

func modernProtocolMetadataError() error {
	return &jsonrpc.Error{
		Code:    jsonrpc.CodeInvalidParams,
		Message: "modern protocol metadata is required for every request",
	}
}
