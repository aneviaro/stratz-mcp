# syntax=docker/dockerfile:1.7
# The release workflow supplies a statically linked linux binary in dist/image/.
FROM scratch AS artifact
ARG TARGETARCH
COPY dist/image/stratz-mcp-linux-${TARGETARCH} /stratz-mcp

FROM scratch
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="stratz-mcp" \
      org.opencontainers.image.description="Unofficial local MCP server for the STRATZ API" \
      org.opencontainers.image.source="https://github.com/aneviaro/stratz-mcp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.licenses="MIT"
COPY --from=artifact /stratz-mcp /stratz-mcp
USER 65532:65532
VOLUME ["/cache"]
ENV XDG_CACHE_HOME=/cache
ENTRYPOINT ["/stratz-mcp"]
CMD ["serve"]
