#!/bin/sh

set -eu

go_command=${GO:-go}
output_dir=${OUTPUT_DIR:-dist}
version=${VERSION:-dev}
revision=${REVISION:-unknown}
schema_version=${SCHEMA_VERSION:-unavailable}
command_package=main
ldflags="-X ${command_package}.version=${version} -X ${command_package}.revision=${revision} -X ${command_package}.schemaVersion=${schema_version}"

mkdir -p "$output_dir"

for target in \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64
do
	goos=${target%/*}
	goarch=${target#*/}
	suffix=
	if [ "$goos" = windows ]; then
		suffix=.exe
	fi

	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		"$go_command" build -trimpath -ldflags "$ldflags" \
		-o "$output_dir/stratz-mcp_${goos}_${goarch}${suffix}" \
		./cmd/stratz-mcp
done
