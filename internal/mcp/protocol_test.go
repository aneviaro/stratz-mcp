package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStdioTransportAdvertisesContractProtocolVersions(t *testing.T) {
	transport := newStdioTransport(nil, nil)
	for _, version := range contracts.SupportedMCPProtocolVersions() {
		if !transport.SupportsProtocolVersion(version) {
			t.Fatalf("SupportsProtocolVersion(%q) = false, want true", version)
		}
	}
	if transport.SupportsProtocolVersion("2099-01-01") {
		t.Fatal("SupportsProtocolVersion(2099-01-01) = true, want false")
	}
}

func TestProtocolMiddlewareHintsByResultType(t *testing.T) {
	tests := []struct {
		name  string
		value sdk.Result
		ttl   int
		scope string
	}{
		{name: "discover", value: &sdk.DiscoverResult{}, ttl: protocolDiscoveryCacheTTL, scope: "public"},
		{name: "tools", value: &sdk.ListToolsResult{}, ttl: protocolCatalogCacheTTL, scope: "public"},
		{name: "prompts", value: &sdk.ListPromptsResult{}, ttl: protocolCatalogCacheTTL, scope: "public"},
		{name: "resources", value: &sdk.ListResourcesResult{}, ttl: protocolCatalogCacheTTL, scope: "public"},
		{name: "resource templates", value: &sdk.ListResourceTemplatesResult{}, ttl: protocolCatalogCacheTTL, scope: "public"},
		{name: "resource read", value: &sdk.ReadResourceResult{}, ttl: 0, scope: "private"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			middleware := protocolMiddleware(func(context.Context, string, sdk.Request) (sdk.Result, error) {
				return test.value, nil
			})
			result, err := middleware(context.Background(), "method is intentionally ignored", nil)
			if err != nil {
				t.Fatal(err)
			}
			cacheable, ok := result.(sdk.CacheableResult)
			if !ok {
				t.Fatalf("result type %T does not expose cache hints", result)
			}
			if cacheable.GetTTLMs() != test.ttl || cacheable.GetCacheScope() != test.scope {
				t.Fatalf(
					"cache hints = %d/%q, want %d/%q",
					cacheable.GetTTLMs(),
					cacheable.GetCacheScope(),
					test.ttl,
					test.scope,
				)
			}
		})
	}
}

func TestProtocolMiddlewareDoesNotCacheErrors(t *testing.T) {
	wantErr := errors.New("handler failed")
	result, err := protocolMiddleware(func(context.Context, string, sdk.Request) (sdk.Result, error) {
		return &sdk.ReadResourceResult{}, wantErr
	})(context.Background(), "resources/read", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	cacheable := result.(*sdk.ReadResourceResult)
	if cacheable.GetTTLMs() != 0 || cacheable.GetCacheScope() != "" {
		t.Fatalf("error result received cache hints: %d/%q", cacheable.GetTTLMs(), cacheable.GetCacheScope())
	}
}
