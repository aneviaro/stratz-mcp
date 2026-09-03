# Future Improvements

## Fight data and per-minute analysis

Add bounded fight data, per-minute analysis, and per-minute statistics to the relevant match and player analysis surfaces. Define the normalized semantics and validate the upstream STRATZ source before exposing the fields; unavailable or unsupported data must remain explicit rather than being fabricated.

## MCP tool-surface refactor

Refactor the tool surface to follow MCP best practices:

- Use slim tool descriptions to minimize context usage.
- Consolidate overlapping capabilities into fewer tools.
- Replace separate `list` and `get` operations with search-oriented tools that support retrieval, filtering, and pagination.

Update the canonical contracts, generated outputs, handlers, tests, and client interoperability checks together when this work is implemented.
