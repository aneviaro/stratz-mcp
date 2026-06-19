Created: 2026-06-19
Purpose: Explain portable installation of generated STRATZ Agent Skills in Codex and Claude.
Status: Generated; do not edit directly

# Installing STRATZ Agent Skills

The five skill directories under `skills/` are portable Agent Skills generated from `workflows/workflows.json`. They contain no vendor-private workflow logic.

## Codex

Copy or symlink each desired directory into `$CODEX_HOME/skills/`, preserving the directory name and `SKILL.md`. Restart Codex or begin a new session so skill discovery runs again.

## Claude

Copy each desired skill directory into the skills location supported by the installed Claude client, preserving the directory name and `SKILL.md`. For clients that import skills through settings, select the whole directory rather than only the Markdown file.

## Generated skills

- `analyze-dota-match`: Analyze a Dota match, optionally focusing on one player.
- `query-stratz`: Design and execute an advanced bounded STRATZ GraphQL query.
- `research-dota-hero`: Research hero reference data and bounded aggregate performance.
- `review-dota-player`: Review a player using a bounded recent-match sample.
- `scout-dota-league`: Scout a league and its professional matches.

Regenerate with `go generate ./...`. Do not edit generated prompt or skill files directly.
