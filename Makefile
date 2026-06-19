GO ?= go
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short=12 HEAD)
SCHEMA_VERSION ?= unavailable

COMMAND_PACKAGE := main
LDFLAGS := -X $(COMMAND_PACKAGE).version=$(VERSION) \
	-X $(COMMAND_PACKAGE).revision=$(REVISION) \
	-X $(COMMAND_PACKAGE).schemaVersion=$(SCHEMA_VERSION)

.PHONY: build check cross-build dev generate test tools vet verify verify-build-info

build:
	mkdir -p dist
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/stratz-mcp ./cmd/stratz-mcp

check: verify vet test verify-build-info

cross-build:
	VERSION="$(VERSION)" REVISION="$(REVISION)" SCHEMA_VERSION="$(SCHEMA_VERSION)" \
		GO="$(GO)" ./scripts/cross-build.sh

dev:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/stratz-mcp

generate:
	$(GO) generate ./...

test:
	$(GO) test ./...

tools:
	mkdir -p .bin
	$(GO) build -o .bin/genqlient github.com/Khan/genqlient

vet:
	$(GO) vet ./...

verify:
	$(GO) mod verify

verify-build-info:
	GO="$(GO)" MAKE="$(MAKE)" ./scripts/verify-build-info.sh
