GO ?= go
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short=12 HEAD)
SCHEMA_VERSION ?= unavailable

COMMAND_PACKAGE := main
EXE := $(if $(filter Windows_NT,$(OS)),.exe,)
LDFLAGS := -X $(COMMAND_PACKAGE).version=$(VERSION) \
	-X $(COMMAND_PACKAGE).revision=$(REVISION) \
	-X $(COMMAND_PACKAGE).schemaVersion=$(SCHEMA_VERSION)

.PHONY: build check check-format check-generated check-restricted cross-build dev generate interop-smoke notices public-import public-readiness test test-live tools vet verify verify-build-info

build:
	mkdir -p dist
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/stratz-mcp$(EXE) ./cmd/stratz-mcp

check: export PAGER := cat
check: export GIT_PAGER := cat
check: verify check-format vet test check-generated public-readiness verify-build-info

check-format:
	@test -z "$$(gofmt -l -- $$(find . -name '*.go' -type f))"

check-generated:
	./scripts/check-generated.sh

check-restricted:
	./scripts/check-restricted-artifacts.sh

public-readiness:
	./scripts/check-public-readiness.sh

public-import:
	./scripts/create-public-import.sh

cross-build:
	VERSION="$(VERSION)" REVISION="$(REVISION)" SCHEMA_VERSION="$(SCHEMA_VERSION)" \
		GO="$(GO)" ./scripts/cross-build.sh

interop-smoke: build
	./scripts/interop-smoke.sh native dist/stratz-mcp

notices:
	GO="$(GO)" ./scripts/generate-notices.sh

dev:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/stratz-mcp

generate:
	$(GO) generate ./...

test:
	$(GO) test ./...

test-live:
	STRATZ_ENV_FILE="$$(cd "$$(dirname "$${STRATZ_ENV_FILE:-.env}")" && pwd)/$$(basename "$${STRATZ_ENV_FILE:-.env}")" \
		$(GO) test -tags=integration -count=1 -v ./integration

tools:
	mkdir -p .bin
	$(GO) build -o .bin/genqlient github.com/Khan/genqlient

vet:
	$(GO) vet ./...

verify:
	$(GO) mod verify

verify-build-info:
	GO="$(GO)" MAKE="$(MAKE)" ./scripts/verify-build-info.sh
