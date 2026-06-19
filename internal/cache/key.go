package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
)

// FormatVersion invalidates incompatible on-disk cache layouts.
const FormatVersion = 1

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDisabled Status = "disabled"
	StatusDegraded Status = "degraded"
)

type LookupStatus string

const (
	LookupHit      LookupStatus = "hit"
	LookupMiss     LookupStatus = "miss"
	LookupStale    LookupStatus = "stale"
	LookupBypass   LookupStatus = "bypass"
	LookupDisabled LookupStatus = "disabled"
)

type Class string

const (
	ClassPublicReference  Class = "public_reference"
	ClassPublicHistorical Class = "public_historical"
	ClassProfileSensitive Class = "profile_sensitive"
	ClassPublicRecent     Class = "public_recent"
	ClassPublicLive       Class = "public_live"
	ClassRawUnclassified  Class = "raw_unclassified"
)

type Classification struct {
	Domain    string
	Class     Class
	TTL       time.Duration
	Stale     time.Duration
	Cacheable bool
	Reason    string
}

type KeyInput struct {
	Namespace     string
	Domain        string
	Class         Class
	Operation     string
	Arguments     any
	Query         string
	Variables     any
	DetailLevel   string
	IncludeRaw    bool
	SchemaVersion string
}

type Key struct {
	Namespace string
	Domain    string
	Class     Class
	Digest    string
}

type LookupRequest struct {
	Key        Key
	Fresh      bool
	AllowStale bool
}

type LookupResult struct {
	Status  LookupStatus
	Payload []byte
	Age     time.Duration
}

type Entry struct {
	Key            Key
	Classification Classification
	Payload        []byte
}

type ClearOptions struct {
	Domain    string
	Namespace string
}

type ClearResult struct {
	Deleted int64
}

type DomainStats struct {
	Domain       string
	Entries      int64
	LogicalBytes int64
	StoredBytes  int64
}

type Stats struct {
	Status        Status
	Reason        string
	FormatVersion int
	Entries       int64
	Namespaces    int64
	LogicalBytes  int64
	StoredBytes   int64
	Domains       []DomainStats
}

var ErrNotCacheable = errors.New("cache entry is not cacheable")

func ResolveClassification(
	cacheConfig config.CacheConfig,
	features config.FeaturesConfig,
	domain string,
	class Class,
	includeRaw bool,
) Classification {
	result := Classification{
		Domain: domainName(domain),
		Class:  class,
	}
	if result.Domain == "" {
		result.Reason = "cache domain is required"
		return result
	}
	if includeRaw {
		result.Reason = "include_raw bypasses cache reads and writes"
		return result
	}
	switch class {
	case ClassPublicReference:
		result.TTL = cacheConfig.PublicReferenceTTL
		result.Stale = cacheConfig.PublicReferenceStale
	case ClassPublicHistorical:
		result.TTL = cacheConfig.PublicHistoricalTTL
		result.Stale = cacheConfig.PublicHistoricalStale
	case ClassProfileSensitive:
		result.TTL = cacheConfig.ProfileSensitiveTTL
		result.Stale = cacheConfig.ProfileSensitiveStale
	case ClassPublicRecent:
		result.TTL = cacheConfig.PublicRecentTTL
		result.Stale = cacheConfig.PublicRecentStale
	case ClassPublicLive:
		result.TTL = cacheConfig.PublicLiveTTL
		result.Stale = cacheConfig.PublicLiveStale
	case ClassRawUnclassified:
		if !features.RawCache {
			result.Reason = "raw GraphQL caching is disabled until field classifications are approved"
			return result
		}
		result.TTL = cacheConfig.RawTTL
		result.Stale = 0
	default:
		result.Reason = "cache class is not defined"
		return result
	}
	if result.TTL <= 0 {
		result.Reason = "cache TTL must be positive"
		return result
	}
	if result.Stale < 0 {
		result.Reason = "cache stale window cannot be negative"
		return result
	}
	result.Cacheable = true
	return result
}

func CanonicalKey(input KeyInput) (Key, error) {
	key := Key{
		Namespace: strings.TrimSpace(input.Namespace),
		Domain:    domainName(input.Domain),
		Class:     input.Class,
	}
	if key.Namespace == "" {
		return Key{}, errors.New("cache namespace is required")
	}
	if key.Domain == "" {
		return Key{}, errors.New("cache domain is required")
	}
	if key.Class == "" {
		return Key{}, errors.New("cache class is required")
	}
	document, err := canonicalDocument(input)
	if err != nil {
		return Key{}, err
	}
	sum := sha256.Sum256(document)
	key.Digest = hex.EncodeToString(sum[:])
	return key, nil
}

func NamespaceForToken(token string) string {
	sum := sha256.Sum256([]byte("stratz-mcp/cache-namespace\x00" + token))
	return hex.EncodeToString(sum[:16])
}

func canonicalDocument(input KeyInput) ([]byte, error) {
	document := map[string]any{
		"arguments":      input.Arguments,
		"class":          input.Class,
		"detail_level":   strings.TrimSpace(input.DetailLevel),
		"domain":         domainName(input.Domain),
		"format_version": FormatVersion,
		"include_raw":    input.IncludeRaw,
		"operation":      strings.TrimSpace(input.Operation),
		"query":          strings.TrimSpace(input.Query),
		"schema_version": strings.TrimSpace(input.SchemaVersion),
		"variables":      input.Variables,
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical cache key: %w", err)
	}
	return data, nil
}

func domainName(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
