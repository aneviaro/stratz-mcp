# Release procedure

Public publishing is disabled until `go run ./cmd/release-clearance-check` succeeds against `docs/release-clearance.json`.

For a candidate:

1. Update generated files and `THIRD_PARTY_NOTICES`.
2. Run `make check`, `go test -race ./...`, `make package`, and native/Docker interoperability smoke tests for both Codex and Claude client profiles.
3. Review vulnerability, OSV, license, and secret-scan results.
4. Create an annotated protected `vMAJOR.MINOR.PATCH` tag in the canonical repository.
5. The release workflow rechecks clearance, builds six native archives and linux/amd64+arm64 images, emits checksums and version metadata, creates SPDX and CycloneDX SBOMs, signs checksums/images keylessly with Sigstore, and records GitHub OIDC provenance.
6. Verify signatures, attestations, archive contents, image labels, non-root execution, read-only operation, and cache persistence before publishing release notes.

Interoperability evidence is produced by `scripts/interop-smoke.sh`. The profile names exercise the shared MCP 2025-11-25 stdio behavior expected by Codex and Claude. A real installed-client check remains a release-operator responsibility and is recorded in the GitHub environment approval.

No long-lived signing key is stored. Publishing jobs have environment protection and minimal permissions.

## Verify a candidate

```sh
cd dist/release
sha256sum -c checksums.txt
cosign verify-blob --bundle checksums.sigstore.json checksums.txt
gh attestation verify ./* --repo aneviaro/stratz-mcp
cosign verify ghcr.io/aneviaro/stratz-mcp@sha256:DIGEST
docker image inspect ghcr.io/aneviaro/stratz-mcp@sha256:DIGEST
docker run --rm --read-only --entrypoint /stratz-mcp \
  ghcr.io/aneviaro/stratz-mcp@sha256:DIGEST version
```

Confirm each archive has `LICENSE`, `THIRD_PARTY_NOTICES`, `VERSION`, and the binary; each archive has SPDX and CycloneDX SBOMs; the image labels match the tag/revision; the runtime user is `65532:65532`; and a mounted `/cache` remains healthy across restart and purge.
