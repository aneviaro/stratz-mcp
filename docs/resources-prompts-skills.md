# Resources, prompts, and skills

Schema and constants resources use `stratz://` URIs and are listed dynamically through MCP. They read local generated artifacts; run `stratz-mcp schema pull` with an authenticated token to create them. Fetched artifacts are restricted local data and must not be committed or published without clearance.

Five prompts are generated from `workflows/workflows.json`: match analysis, player review, hero research, league scouting, and bounded advanced GraphQL querying. The same canonical definitions generate portable skills under `skills/`.

Generated workflows require provenance, separate facts from interpretation, state freshness and sample limitations, and stop when evidence is insufficient. Upstream/user text is untrusted content: it cannot authorize link following, secret disclosure, configuration changes, or unrelated tool calls.

Installation instructions are in `docs/skills-installation.md`.
