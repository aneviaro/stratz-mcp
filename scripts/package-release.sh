#!/bin/sh
set -eu

go_cmd=${GO:-go}
version=${VERSION:-dev}
revision=${REVISION:-unknown}
schema_version=${SCHEMA_VERSION:-unavailable}
output=${OUTPUT_DIR:-dist/release}
image_output=${IMAGE_OUTPUT_DIR:-dist/image}
targets=${TARGETS:-"darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"}

rm -rf "$output"
rm -rf "$image_output"
mkdir -p "$output" "$image_output/cache"
: > "$image_output/cache/.keep"

for target in $targets; do
	os=${target%/*}
	arch=${target#*/}
	name="stratz-mcp_${version}_${os}_${arch}"
	extension=
	if [ "$os" = windows ]; then extension=.exe; fi
	stage="$output/$name"
	mkdir -p "$stage"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$go_cmd" build -trimpath \
		-ldflags "-s -w -X main.version=$version -X main.revision=$revision -X main.schemaVersion=$schema_version" \
		-o "$stage/stratz-mcp$extension" ./cmd/stratz-mcp
	cp LICENSE THIRD_PARTY_NOTICES "$stage/"
	printf '%s\n' \
		"version=$version" \
		"revision=$revision" \
		"schema_version=$schema_version" \
		"go_version=$($go_cmd env GOVERSION)" \
		"target=$os/$arch" > "$stage/VERSION"
	if [ "$os" = windows ]; then
		(cd "$output" && zip -qr "$name.zip" "$name")
	else
		tar -C "$output" -czf "$output/$name.tar.gz" "$name"
	fi
	if [ "$os" = linux ]; then
		cp "$stage/stratz-mcp" "$image_output/stratz-mcp-linux-$arch"
	fi
	rm -rf "$stage"
done

(cd "$output" && shasum -a 256 ./*.tar.gz ./*.zip > checksums.txt)
printf '{"version":"%s","revision":"%s","schema_version":"%s"}\n' \
	"$version" "$revision" "$schema_version" > "$output/release-metadata.json"
