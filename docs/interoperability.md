# Interoperability record

Date: 2026-06-19

Automated fixtures:

- Codex profile, native stdio: passed on 2026-06-19. `scripts/interop-smoke.sh` validated initialization, tool/resource/prompt discovery, `stratz_server_info`, JSON-RPC-only stdout, and credential non-disclosure.
- Claude profile, native stdio: passed on 2026-06-19 with a distinct client identity.
- Codex and Claude Docker profiles: local execution skipped on 2026-06-19 because the Docker daemon was unavailable. The required CI job is configured to run both profiles against a non-root, read-only scratch image; its protected-environment result still needs to be recorded.

These are deterministic protocol compatibility checks, not claims that a locally installed proprietary client UI was controlled in this repository environment. Real-client native and Docker checks are required in the protected release environment before approval.
