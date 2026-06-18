package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const endpoint = "https://api.stratz.com/graphql"

func main() {
	token := strings.TrimSpace(os.Getenv("STRATZ_API_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr, "STRATZ_API_TOKEN is not set")
		os.Exit(2)
	}

	payload, err := json.Marshal(map[string]any{
		"query":         "query GoProbe { __typename }",
		"operationName": "GoProbe",
		"variables":     map[string]any{},
	})
	if err != nil {
		fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		fail(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/graphql-response+json, application/json")
	req.Header.Set("User-Agent", "stratz-go-probe/0.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		fail(err)
	}

	contentType := resp.Header.Get("Content-Type")
	fmt.Printf("HTTP status: %s\n", resp.Status)
	fmt.Printf("Content-Type: %s\n", contentType)
	fmt.Printf("Server: %s\n", resp.Header.Get("Server"))
	fmt.Printf("cf-mitigated: %s\n", resp.Header.Get("cf-mitigated"))
	fmt.Printf("cf-ray: %s\n", resp.Header.Get("cf-ray"))

	if resp.Header.Get("cf-mitigated") == "challenge" ||
		(resp.StatusCode == http.StatusForbidden &&
			strings.Contains(strings.ToLower(contentType), "text/html")) {
		fmt.Println("Result: BLOCKED by Cloudflare before STRATZ handled the GraphQL request")
		os.Exit(1)
	}

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("Result: unexpected non-JSON response\nBody prefix: %.300s\n", body)
		os.Exit(1)
	}

	pretty, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Printf("Result: STRATZ returned JSON\n%s\n", pretty)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "probe failed: %v\n", err)
	os.Exit(1)
}
