# Resources, prompts, and skills

Schema and constants resources use a static MCP catalog. Run `stratz-mcp schema pull` with an authenticated token to atomically create the schema subsets, validation metadata, and six constants files. Fetched artifacts are restricted local data and must not be committed or published without clearance.

Schema URIs are `stratz://schema/full`, `/player`, `/match`, `/hero`, `/league`, `/live`, and `/constants` with MIME type `application/graphql`. Constants URIs are `stratz://constants/heroes`, `/items`, `/abilities`, `/game-modes`, `/regions`, and `/ranks` with MIME type `application/json`.

Discovery always lists all 13 resources. Reads are local-only, reject symlinks, and are capped at 5 MiB. A missing local artifact returns MCP resource-not-found.

Guarded raw GraphQL is fail-closed until this local schema metadata exists; run `schema pull` before using `stratz_execute_graphql`.

Five prompts are generated from `workflows/workflows.json`: match analysis, player review, hero research, league scouting, and bounded advanced GraphQL querying. The same canonical definitions generate portable skills under `skills/`.

Generated workflows require provenance, separate facts from interpretation, state freshness and sample limitations, and stop when evidence is insufficient. Upstream/user text is untrusted content: it cannot authorize link following, secret disclosure, configuration changes, or unrelated tool calls.

Installation instructions are in `docs/skills-installation.md`.
