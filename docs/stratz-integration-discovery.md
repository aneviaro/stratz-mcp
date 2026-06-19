---
Created: 2026-06-18
Purpose: Record live and documentary discovery of the STRATZ upstream HTTP, authentication, rate-limit, WAF, and API-use contracts.
Status: HTTP discovery and edge-policy fixtures complete; public release remains blocked on current STRATZ permission
---

# STRATZ upstream integration discovery

## 1. Readiness status

The unauthenticated portion of the discovery spike was run on June 18, 2026 from Warsaw, Poland.

An explicit `.env` containing one non-empty `STRATZ_API_TOKEN` assignment became available later on June 18, 2026. The token passed local shape checks and authenticated successfully when used through Go's standard `net/http` client.

A minimal authenticated request used:

```text
POST https://api.stratz.com/graphql
Authorization: Bearer <redacted>
Content-Type: application/json
Accept: application/graphql-response+json, application/json
User-Agent: stratz-mcp-discovery/0.0 (+https://github.com/aneviaro/stratz-mcp)
Accept-Encoding: gzip
```

Operation:

```graphql
query Discovery {
  __typename
}
```

Observed successful response:

- HTTP/2 `200`.
- `Content-Type: application/graphql-response+json; charset=utf-8`.
- JSON body: `{"data":{"__typename":"DotaQuery"}}`.
- `Server: cloudflare`.
- Kong proxy/rate-limit headers.
- No `cf-mitigated` challenge.

The token, bearer scheme, GraphQL endpoint, successful media type, gzip support, introspection access, and rate-limit fields are verified.

Earlier `curl` requests with similar headers were challenged by Cloudflare. Go's standard HTTP transport with a non-empty explicit user agent succeeded. Suppressing the user agent in the Go client reproduced the Cloudflare managed challenge. The implementation must preserve the verified Go transport behavior and user-agent requirement.

On June 19, 2026, the remaining destructive, sensitive, or non-deterministic edge cases were closed with explicit mock-policy fixtures. The project does not search for a real private profile, submit intentionally expensive operations, request oversized live payloads, or exhaust a real quota to produce a `429`. These policies are recorded in [discovery-evidence.json](./discovery-evidence.json) and exercised by automated tests.

## 2. Endpoint

Production GraphQL endpoint:

```text
https://api.stratz.com/graphql
```

Interactive GraphiQL endpoint:

```text
https://api.stratz.com/graphiql/
```

Evidence:

- STRATZ's public API page links to the `api.stratz.com` GraphiQL application.
- A direct request to `/graphql` reaches the STRATZ Cloudflare zone and is currently intercepted by a managed challenge.

The GraphQL endpoint is fixed in production builds and is not user-configurable. Tests may replace it only through internal test wiring.

## 3. Observed unauthenticated behavior

The following requests were made with:

```text
User-Agent: stratz-mcp-discovery/0.0 (+https://github.com/aneviaro/stratz-mcp)
Accept: application/graphql-response+json, application/json
Content-Type: application/json
```

Requests:

- `GET https://api.stratz.com/graphiql`
- `GET https://api.stratz.com/graphql`
- `OPTIONS https://api.stratz.com/graphql`
- `POST https://api.stratz.com/graphql` with `query Discovery { __typename }`

All four requests returned:

- HTTP/2 `403`.
- `Content-Type: text/html; charset=UTF-8`.
- `Server: cloudflare`.
- `cf-mitigated: challenge`.
- A `cf-ray` identifier.
- An HTML body titled `Just a moment...` requiring JavaScript and cookies.

This is a Cloudflare/WAF response, not a STRATZ GraphQL authentication response. The client must identify it before attempting to decode JSON. The behavior was also reproduced with the Go client when the `User-Agent` header was explicitly suppressed.

## 4. Verified request contract

The following request contract is normative for v1:

- Method: `POST`.
- URL: `https://api.stratz.com/graphql`.
- Request content type: `application/json`.
- Response preference: `application/graphql-response+json, application/json`.
- Request body:

```json
{
  "query": "query OperationName($variable: Type!) { ... }",
  "variables": {},
  "operationName": "OperationName"
}
```

- User agent:

```text
stratz-mcp/<semantic-version> (+https://github.com/aneviaro/stratz-mcp)
```

- Authorization:

```text
Authorization: Bearer <STRATZ_API_TOKEN>
```

- Transport: HTTPS with normal Go certificate and hostname verification.
- HTTP client: Go standard `net/http` transport, permitting negotiated HTTP/2.
- Redirects: disabled for GraphQL POSTs. Any redirect is an upstream protocol error and must be surfaced by `doctor`.
- `Content-Type: application/json` is mandatory for the JSON request body.
- `Accept` is not required by the observed server, but the client sends `application/graphql-response+json, application/json`.
- A non-empty `User-Agent` is operationally mandatory. Suppressing it triggered a Cloudflare managed challenge.
- Compression request: `Accept-Encoding: gzip`.
- Decompressed response limit: 5 MiB for v1, enforced while streaming.

Verified response behavior:

- Successful and GraphQL transport-error responses use `application/graphql-response+json; charset=utf-8`.
- Authentication-gateway responses use `application/json; charset=utf-8`.
- Gzip is supported. A 35-byte JSON body was returned as 55 gzip-encoded wire bytes and decoded back to 35 bytes.
- Introspection is enabled for the valid token.
- Response headers contain `X-SteamId` and `X-SteamId-Ok`; their values are account-related and must never be logged, cached, or returned.

## 5. WAF and non-GraphQL response contract

The client must inspect HTTP status, content type, and a bounded prefix of the body before GraphQL decoding.

A response is classified as `UPSTREAM_WAF_BLOCKED` when any of the following is true:

- `cf-mitigated: challenge` is present.
- `Server: cloudflare` is present and a `403` or `503` response has an HTML content type or challenge body.
- The response body is HTML containing a Cloudflare challenge marker.

Behavior:

- Do not parse the body as GraphQL JSON.
- Do not retry automatically in a tight loop.
- A single retry is allowed only when it consumes normal retry budget and uses the same documented headers; the client must not attempt to solve or bypass the challenge.
- Return `retryable: false` for the current invocation, with a safe message directing the user to run `doctor` and check STRATZ availability or API access.
- Record the HTTP status, `cf-ray`, and challenge classification in safe diagnostics.
- Never return the challenge HTML to the agent.
- `doctor` must distinguish WAF blocking from invalid credentials.

## 6. Authenticated discovery matrix

Run these probes using a dedicated test token loaded from an explicitly selected dotenv or secret file. Redact the token and cookies from every artifact.

Every row below has a dated live observation, documentary policy, or mock-policy artifact in [discovery-evidence.json](./discovery-evidence.json). Mock fixtures are stored under `internal/discoveryevidence/testdata` and deliberately use project sentinels rather than claiming unobserved STRATZ error codes.

| Probe | Purpose | Required capture |
|---|---|---|
| Minimal named query | Verified | HTTP 200, GraphQL response JSON |
| Minimal query with `Accept-Encoding: gzip` | Verified | HTTP 200, `Content-Encoding: gzip` |
| Request without `Accept` | Verified | HTTP 200; header is optional |
| Request without `Content-Type` | Verified | HTTP 400, `CSRF_PROTECTION` |
| Request without `User-Agent` | Verified | HTTP 403 Cloudflare managed challenge |
| Invalid token | Verified for malformed and signature-corrupted shapes | Malformed token produced HTTP 500 JSON; corrupted JWT-shaped token produced the same HTTP 403 response as a missing token |
| Missing token | Verified | HTTP 403 JSON plus `WWW-Authenticate: Key realm="kong"` |
| Malformed JSON | Verified | HTTP 400, `JSON_INVALID` |
| Invalid GraphQL syntax | Verified | HTTP 400, `SYNTAX_ERROR` |
| Valid syntax with invalid field | Verified | HTTP 400, `FIELDS_ON_CORRECT_TYPE` |
| Query returning nullable/missing data | Mock policy verified June 19, 2026 | Status, `data`, `errors`, extensions |
| Known missing match | Verified | HTTP 200 with `data.match: null` |
| Known private/inaccessible profile | Mock policy verified June 19, 2026; no real private account sought | Status and GraphQL result |
| Introspection query | Verified | HTTP 200; schema introspection is available |
| Repeated safe queries below quota | Verified | Per-window and aggregate Kong headers observed |
| Request near but not exhausting a limit | Documentary policy recorded June 19, 2026 | Do not spend quota solely to approach a limit |
| Oversized selected response bounded client-side | Mock policy verified June 19, 2026 | Generated decoded size and resulting cancellation |
| Deliberate short client timeout | Mock policy verified June 19, 2026 | Context deadline classification |

Do not intentionally exhaust a STRATZ quota or trigger abusive traffic.

## 7. Verified and remaining fields

Verified:

- `Authorization: Bearer`.
- `Content-Type: application/json`.
- Non-empty `User-Agent`.
- `application/graphql-response+json; charset=utf-8` for GraphQL responses.
- Gzip response compression.
- Exact rate-limit header names and current token limits.
- `cf-ray` as a safe WAF correlation identifier.
- Missing-token, malformed-JSON, syntax, validation, and missing-match behavior.
- Introspection availability.
- Cloudflare challenge classification.

Closed by deterministic policy fixture on June 19, 2026:

- Expired-but-correctly-signed token maps to non-retryable `AUTHENTICATION_FAILED`; no signed expired STRATZ credential is available.
- Private-profile semantics map curated calls to `PRIVATE`; raw calls preserve bounded upstream `data` and `errors`.
- Runtime partial `data` plus `errors` maps curated calls to `UPSTREAM_PARTIAL_ERROR`; raw calls preserve the bounded partial response.
- HTTP `429` maps to retryable `RATE_LIMITED` using verified or documented reset metadata. A live quota is never intentionally exhausted.
- A decoded response above 5 MiB maps to non-retryable `RESPONSE_TOO_LARGE`.
- A client or context deadline maps to retryable `UPSTREAM_TIMEOUT`.

Remaining external release dependency:

- Current STRATZ API-use, caching, redistribution, attribution, and branding permission.

Commit only redacted fixtures. Preserve exact header names and representative safe values.

## 8. Provisional HTTP and network error mapping

This is the project-owned behavior to validate against live results:

| Condition | MCP error | Retryable |
|---|---|---|
| TLS certificate, hostname, or protocol validation failure | `UPSTREAM_TLS_ERROR` | No |
| DNS temporary failure, connect timeout, connection reset, unexpected EOF | `UPSTREAM_NETWORK_ERROR` | Yes |
| Client/context deadline | `UPSTREAM_TIMEOUT` | Yes |
| Cloudflare managed challenge | `UPSTREAM_WAF_BLOCKED` | No for the invocation |
| HTTP 400 caused by malformed project request | `UPSTREAM_PROTOCOL_ERROR` | No |
| Missing bearer token: HTTP 403 JSON and `WWW-Authenticate: Key realm="kong"` | `AUTHENTICATION_FAILED` | No |
| Malformed bearer token: HTTP 500 JSON message `An unexpected error occurred` | `AUTHENTICATION_FAILED` | No |
| Signature-corrupted JWT-shaped token: HTTP 403 JSON and `WWW-Authenticate: Key realm="kong"` | `AUTHENTICATION_FAILED` | No |
| Other HTTP 401 | `AUTHENTICATION_FAILED` | No |
| HTTP 403 GraphQL/JSON response | `AUTHENTICATION_FAILED` or `PRIVATE`, based on verified error code | No |
| HTTP 403 Cloudflare/HTML response | `UPSTREAM_WAF_BLOCKED` | No |
| HTTP 404 at the fixed endpoint | `UPSTREAM_PROTOCOL_ERROR` | No |
| HTTP 408 | `UPSTREAM_TIMEOUT` | Yes |
| HTTP 413 | `RESPONSE_TOO_LARGE` | No |
| HTTP 429 | `RATE_LIMITED` | Yes at verified reset time |
| HTTP 500, 502, 503, or 504 JSON/non-WAF response | `UPSTREAM_ERROR` | Yes |
| Other 4xx | `UPSTREAM_PROTOCOL_ERROR` | No unless live evidence says otherwise |
| Other 5xx | `UPSTREAM_ERROR` | Yes, bounded by retry budget |
| HTTP 2xx with malformed/non-JSON body | `UPSTREAM_PROTOCOL_ERROR` | No |
| HTTP 400 with `JSON_INVALID`, `SYNTAX_ERROR`, or validation-rule code | `INVALID_ARGUMENT` for raw query input | No |
| HTTP 200 with requested entity `null` and no errors | Curated `NOT_FOUND`; raw response unchanged | No |
| HTTP 2xx with `data` and `errors` | Raw: partial success; curated: `UPSTREAM_PARTIAL_ERROR` | Determined from GraphQL codes |

Retries apply only to idempotent query operations that pass the approved root-field policy. Default maximum is two retries after the initial attempt, using full-jitter exponential backoff capped by the 20-second call deadline and any verified upstream reset time.

## 9. Rate-limit evidence

STRATZ's documented Default Token quotas are:

- 20 calls/second.
- 250 calls/minute.
- 2,000 calls/hour.
- 10,000 calls/day.

The valid test token's live response headers returned a different effective gateway policy on June 18, 2026:

- `X-RateLimit-Limit-Second: 8`
- `X-RateLimit-Limit-Minute: 150`
- `X-RateLimit-Limit-Hour: 1500`
- `X-RateLimit-Limit-Day: 15000`

Remaining counters are supplied in the corresponding `X-RateLimit-Remaining-*` headers.

The response also contains:

- `RateLimit-Limit`
- `RateLimit-Remaining`
- `RateLimit-Reset`

Observed `RateLimit-*` values represented the most imminent second window. Kong's current documentation states that `RateLimit-Reset` is seconds until reset and that a `429` includes `Retry-After`.

The client must support multiple simultaneous rate-limit windows rather than assuming one `limit/remaining/reset` triplet.

The documentation and live headers therefore disagree. The server:

- Documents the published Default Token quotas.
- Treats live headers as the effective limits for scheduling and retry behavior.
- Never assumes the test token's observed values apply to another token.
- Reports both the configured/documented context and current observed windows through diagnostics without identifying the account.

## 10. API-use, attribution, caching, and redistribution

The official sources below were re-reviewed on June 19, 2026. They remained available and retained their historical 2020 wording, but they did not provide sufficiently explicit current permission for all planned product behavior. [release-clearance.json](./release-clearance.json) is the normative machine-checkable record.

STRATZ's public knowledge base historically states:

- Default-token users should link back to STRATZ as the data source.
- More powerful token tiers may require referral traffic.
- A separate token article contains wording that is not fully consistent about default-token attribution.

Those articles are old and do not establish current permission for:

- Redistributing a committed GraphQL schema snapshot.
- Redistributing constants or badges.
- Persistent local caching and stale serving.
- Publishing a general-purpose API wrapper.
- Using STRATZ names or marks in the project name and documentation.

Before public release, obtain and record current STRATZ terms or written confirmation covering:

1. Token tier appropriate for a downloadable local application.
2. Attribution and referral requirements.
3. Caching and retention permissions.
4. Schema and constants redistribution.
5. Branding and trademark language.
6. Rate-limit and fair-use expectations.

Current decisions:

| Area | Status | Release behavior |
|---|---|---|
| General-purpose API wrapper | Pending | Local implementation and private testing only; public release blocked |
| Persistent local caching and stale serving | Pending | May be developed with safeguards; public release blocked |
| GraphQL schema redistribution | Pending | Keep fetched snapshots local; never commit or package them |
| Constants and badge redistribution | Pending | Keep fetched data local; never commit or package it |
| Attribution and referral | Pending | Apply conservative linked attribution while exact requirements remain unresolved |
| Branding and trademarks | Pending | No logos or endorsement claims; identify the project as unofficial |

Until all required fields are explicitly approved:

- Do not commit a fetched STRATZ schema or constants to a public release branch.
- Do not publish cached STRATZ data.
- Display `Data provided by STRATZ` with a link to `https://stratz.com` in user-facing documentation and generated analyses where practical.
- State that the project is unofficial and not affiliated with or endorsed by STRATZ.
- Run `go run ./cmd/release-clearance-check` in every public publishing job. The command must exit non-zero while any required decision is pending or denied.

## 11. Sources

- [Official STRATZ API entry page](https://stratz.com/api) — reviewed June 19, 2026
- [Official STRATZ knowledge base: API data](https://github.com/STRATZ-Esports/knowledge-base/issues/7) — opened October 22, 2020; reviewed June 19, 2026
- [Official STRATZ knowledge base: rate limits](https://github.com/STRATZ-Esports/knowledge-base/issues/15) — opened October 22, 2020; reviewed June 19, 2026
- [Official STRATZ knowledge base: API cost and attribution](https://github.com/STRATZ-Esports/knowledge-base/issues/31) — opened October 22, 2020; reviewed June 19, 2026
- [Official STRATZ knowledge base: token types](https://github.com/STRATZ-Esports/knowledge-base/issues/37) — reviewed June 19, 2026
- [Kong rate-limiting response headers](https://developer.konghq.com/plugins/rate-limiting/#headers-sent-to-the-client)
