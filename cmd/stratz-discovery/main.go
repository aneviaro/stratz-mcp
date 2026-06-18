package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	endpoint           = "https://api.stratz.com/graphql"
	maxWireBody        = 6 << 20
	maxDecodedBody     = 5 << 20
	requestTimeout     = 20 * time.Second
	discoveryUserAgent = "stratz-mcp-discovery/0.1 (+https://github.com/aneviaro/stratz-mcp)"
)

type probe struct {
	name              string
	token             string
	body              []byte
	acceptEncoding    string
	omitAccept        bool
	omitContentType   bool
	suppressUserAgent bool
}

type observation struct {
	Name               string              `json:"name"`
	Status             int                 `json:"status"`
	Protocol           string              `json:"protocol"`
	HeaderNames        []string            `json:"header_names"`
	Headers            map[string][]string `json:"headers"`
	WireBytes          int                 `json:"wire_bytes"`
	DecodedBytes       int                 `json:"decoded_bytes"`
	ContentEncoding    string              `json:"content_encoding,omitempty"`
	BodyClassification string              `json:"body_classification"`
	JSONShape          any                 `json:"json_shape,omitempty"`
	SafeJSONDetails    any                 `json:"safe_json_details,omitempty"`
}

func main() {
	token := strings.TrimSpace(os.Getenv("STRATZ_API_TOKEN"))
	if token == "" {
		fatal(errors.New("STRATZ_API_TOKEN is not set"))
	}

	validQuery := mustJSON(map[string]any{
		"query":         "query Discovery { __typename }",
		"operationName": "Discovery",
		"variables":     map[string]any{},
	})
	introspectionQuery := mustJSON(map[string]any{
		"query":         "query DiscoverySchema { __schema { queryType { name } mutationType { name } subscriptionType { name } directives { name } } }",
		"operationName": "DiscoverySchema",
		"variables":     map[string]any{},
	})

	probes := []probe{
		{name: "valid_minimal_identity", token: token, body: validQuery},
		{name: "valid_minimal_gzip", token: token, body: validQuery, acceptEncoding: "gzip"},
		{name: "valid_without_accept", token: token, body: validQuery, omitAccept: true},
		{name: "valid_without_content_type", token: token, body: validQuery, omitContentType: true},
		{name: "valid_without_user_agent", token: token, body: validQuery, suppressUserAgent: true},
		{name: "missing_token", body: validQuery},
		{name: "invalid_token", token: "invalid-discovery-token", body: validQuery},
		{name: "invalid_jwt_shaped_token", token: corruptToken(token), body: validQuery},
		{name: "malformed_json", token: token, body: []byte(`{"query":`)},
		{name: "invalid_graphql_syntax", token: token, body: mustJSON(map[string]any{
			"query":         "query Broken {",
			"operationName": "Broken",
			"variables":     map[string]any{},
		})},
		{name: "invalid_graphql_field", token: token, body: mustJSON(map[string]any{
			"query":         "query InvalidField { fieldThatCannotExist }",
			"operationName": "InvalidField",
			"variables":     map[string]any{},
		})},
		{name: "introspection", token: token, body: introspectionQuery},
		{name: "missing_match", token: token, body: mustJSON(map[string]any{
			"query":         "query MissingMatch { match(id: 1) { id } }",
			"operationName": "MissingMatch",
			"variables":     map[string]any{},
		})},
		{name: "missing_player", token: token, body: mustJSON(map[string]any{
			"query":         "query MissingPlayer { player(steamAccountId: 1) { steamAccountId } }",
			"operationName": "MissingPlayer",
			"variables":     map[string]any{},
		})},
	}

	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	for _, p := range probes {
		if !selected(p.name) {
			continue
		}
		obs, err := runProbe(client, p)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", p.name, err))
		}
		if err := encoder.Encode(obs); err != nil {
			fatal(err)
		}
	}

	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("rate_header_sample_%d", i)
		if !selected(name) {
			continue
		}
		obs, err := runProbe(client, probe{
			name:  name,
			token: token,
			body:  validQuery,
		})
		if err != nil {
			fatal(err)
		}
		if err := encoder.Encode(obs); err != nil {
			fatal(err)
		}
	}
}

func selected(name string) bool {
	filter := strings.TrimSpace(os.Getenv("STRATZ_DISCOVERY_ONLY"))
	if filter == "" {
		return true
	}
	for _, candidate := range strings.Split(filter, ",") {
		if strings.TrimSpace(candidate) == name {
			return true
		}
	}
	return false
}

func corruptToken(token string) string {
	if token == "" {
		return "invalid"
	}
	last := token[len(token)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	return token[:len(token)-1] + string(replacement)
}

func runProbe(client *http.Client, p probe) (observation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(p.body))
	if err != nil {
		return observation{}, err
	}
	if !p.omitContentType {
		req.Header.Set("Content-Type", "application/json")
	}
	if !p.omitAccept {
		req.Header.Set("Accept", "application/graphql-response+json, application/json")
	}
	if p.suppressUserAgent {
		req.Header.Set("User-Agent", "")
	} else {
		req.Header.Set("User-Agent", discoveryUserAgent)
	}
	if p.acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", p.acceptEncoding)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return observation{}, err
	}
	defer resp.Body.Close()

	wireBody, err := io.ReadAll(io.LimitReader(resp.Body, maxWireBody+1))
	if err != nil {
		return observation{}, err
	}
	if len(wireBody) > maxWireBody {
		return observation{}, errors.New("wire response exceeded discovery limit")
	}

	decodedBody := wireBody
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		reader, err := gzip.NewReader(bytes.NewReader(wireBody))
		if err != nil {
			return observation{}, err
		}
		defer reader.Close()
		decodedBody, err = io.ReadAll(io.LimitReader(reader, maxDecodedBody+1))
		if err != nil {
			return observation{}, err
		}
	}
	if len(decodedBody) > maxDecodedBody {
		return observation{}, errors.New("decoded response exceeded discovery limit")
	}

	headerNames := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		headerNames = append(headerNames, strings.ToLower(name))
	}
	sort.Strings(headerNames)

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	classification := "non_json"
	var shape any
	var safeDetails any
	if strings.Contains(contentType, "json") {
		classification = "json"
		shape = jsonShape(decodedBody)
		safeDetails = safeJSONDetails(decodedBody)
	} else if resp.Header.Get("cf-mitigated") == "challenge" ||
		strings.Contains(strings.ToLower(string(prefix(decodedBody, 512))), "just a moment") {
		classification = "cloudflare_challenge"
	}

	return observation{
		Name:               p.name,
		Status:             resp.StatusCode,
		Protocol:           resp.Proto,
		HeaderNames:        headerNames,
		Headers:            safeHeaders(resp.Header),
		WireBytes:          len(wireBody),
		DecodedBytes:       len(decodedBody),
		ContentEncoding:    resp.Header.Get("Content-Encoding"),
		BodyClassification: classification,
		JSONShape:          shape,
		SafeJSONDetails:    safeDetails,
	}, nil
}

func jsonShape(body []byte) any {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return map[string]any{"valid_json": false}
	}
	return shape(value, 0)
}

func shape(value any, depth int) any {
	if depth >= 4 {
		return "<truncated>"
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make(map[string]any, len(keys))
		for _, key := range keys {
			result[key] = shape(typed[key], depth+1)
		}
		return result
	case []any:
		result := map[string]any{"length": len(typed)}
		if len(typed) > 0 {
			result["first"] = shape(typed[0], depth+1)
		}
		return result
	case nil:
		return nil
	case string:
		return "<string>"
	case float64:
		return "<number>"
	case bool:
		return "<boolean>"
	default:
		return fmt.Sprintf("<%T>", typed)
	}
}

func safeJSONDetails(body []byte) any {
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil
	}

	result := map[string]any{}
	if message, ok := value["message"].(string); ok {
		result["message"] = truncate(message, 512)
	}
	if errorsValue, ok := value["errors"].([]any); ok {
		safeErrors := make([]any, 0, len(errorsValue))
		for _, item := range errorsValue {
			errorObject, ok := item.(map[string]any)
			if !ok {
				continue
			}
			safeError := map[string]any{}
			if message, ok := errorObject["message"].(string); ok {
				safeError["message"] = truncate(message, 512)
			}
			if extensions, ok := errorObject["extensions"].(map[string]any); ok {
				safeExtensions := map[string]any{}
				for _, key := range []string{"code", "codes", "number"} {
					if value, exists := extensions[key]; exists {
						safeExtensions[key] = value
					}
				}
				if len(safeExtensions) > 0 {
					safeError["extensions"] = safeExtensions
				}
			}
			safeErrors = append(safeErrors, safeError)
		}
		result["errors"] = safeErrors
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func safeHeaders(headers http.Header) map[string][]string {
	safe := make(map[string][]string)
	for name, values := range headers {
		lower := strings.ToLower(name)
		switch {
		case lower == "content-type",
			lower == "content-encoding",
			lower == "content-length",
			lower == "access-control-expose-headers",
			lower == "server",
			lower == "cf-mitigated",
			lower == "cf-ray",
			lower == "retry-after",
			lower == "www-authenticate",
			lower == "request-id",
			lower == "x-request-id",
			strings.Contains(lower, "rate"),
			strings.Contains(lower, "limit"),
			strings.Contains(lower, "quota"):
			safe[lower] = append([]string(nil), values...)
		}
	}
	return safe
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func prefix(body []byte, limit int) []byte {
	if len(body) <= limit {
		return body
	}
	return body[:limit]
}

func mustJSON(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return body
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "discovery failed:", err)
	os.Exit(1)
}
