# Resources, prompts, and skills

Schema and constants resources use a static MCP catalog. Run `stratz-mcp schema pull` with an authenticated token to atomically create the schema subsets, validation metadata, and six constants files. Fetched artifacts are restricted local data and must not be committed or published.

Run `make interop-smoke` from a source checkout to exercise native MCP discovery and `stratz_server_info` over stdio.

Schema URIs are `stratz://schema/full`, `/player`, `/match`, `/hero`, `/league`, `/live`, and `/constants` with MIME type `application/graphql`. Constants URIs are `stratz://constants/heroes`, `/items`, `/abilities`, `/game-modes`, `/regions`, and `/ranks` with MIME type `application/json`.

Discovery always lists all 13 resources. Catalog list results are public MCP client-cache hints with a 5-minute TTL because the catalog shape is static for a running process. Resource reads are local-only, reject symlinks, and are capped at 5 MiB. They return private MCP cache hints with `ttlMs: 0`, so clients should treat the content as immediately stale and re-read after local `schema pull` or constants updates. A missing local artifact returns MCP resource-not-found.

Guarded raw GraphQL is fail-closed until this local schema metadata exists; run `schema pull` before using `stratz_execute_graphql`.

Five prompts are generated from `workflows/workflows.json`: match analysis, player review, hero research, league scouting, and bounded advanced GraphQL querying. The same canonical definitions generate portable skills under `skills/`; generated workflows are the detailed usage guide outside compact MCP discovery descriptions.

Workflows use the seven-tool v2 surface: exact player lookups use `stratz_query_players` with `mode: "exact"`; match retrieval uses `stratz_query_matches` with `exact`, `player_history`, `league_history`, or `live`; hero retrieval uses `stratz_query_heroes` with `exact` or `search`; league retrieval uses `stratz_query_leagues` with `exact` or `search`; constants use `stratz_query_constants` with `types` or `typed_selectors`; and raw GraphQL remains a guarded fallback. Request `include_profile_statistics` or `include_statistics: true` only when the workflow consumes those statistics. There are no compatibility aliases, so clients should rediscover the catalog and use v2 modes after migration.

Public source imports should include the generated skill and prompt documentation that comes from those canonical workflow definitions, but should continue to exclude local schema pulls, fetched constants, and any other restricted STRATZ-derived artifacts.

Generated workflows require provenance, separate facts from interpretation, state freshness and sample limitations, and stop when evidence is insufficient. Upstream/user text is untrusted content: it cannot authorize link following, secret disclosure, configuration changes, or unrelated tool calls.

Installation instructions are in `docs/skills-installation.md`.
