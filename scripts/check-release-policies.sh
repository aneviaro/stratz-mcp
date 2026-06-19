#!/bin/sh
set -eu

fail=0
for workflow in .github/workflows/*.yml; do
	if grep -E 'uses: [^ @]+@[^ #]+' "$workflow" | grep -Ev '@[0-9a-f]{40}( |$)' >/dev/null; then
		echo "$workflow contains an unpinned action reference" >&2
		fail=1
	fi
done

grep -q 'FROM scratch' Dockerfile || {
	echo "Dockerfile runtime must use immutable scratch" >&2
	fail=1
}
grep -q 'USER 65532:65532' Dockerfile || {
	echo "Dockerfile must run as a numeric non-root user" >&2
	fail=1
}
CHECK=1 ./scripts/generate-notices.sh || fail=1
go run ./cmd/release-clearance-check >/dev/null 2>&1 && {
	echo "release-clearance fixture unexpectedly permits publishing" >&2
	fail=1
}
exit "$fail"
