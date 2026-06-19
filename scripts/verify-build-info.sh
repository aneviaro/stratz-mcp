#!/bin/sh

set -eu

make_command=${MAKE:-make}
expected="stratz-mcp version=v0.0.0-test revision=test-revision schema_version=sha256:test"

"$make_command" build \
	VERSION=v0.0.0-test \
	REVISION=test-revision \
	SCHEMA_VERSION=sha256:test

actual=$(./dist/stratz-mcp version)
if [ "$actual" != "$expected" ]; then
	echo "build metadata mismatch: got \"$actual\", want \"$expected\"" >&2
	exit 1
fi
