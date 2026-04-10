GO = mise exec go@1.26.2 -- go
GO_ENV = GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache
VERSION ?= $(shell grep 'version:' mix.exs | sed 's/.*"\(.*\)"/\1/')

.PHONY: all build test clean release

all: build

build: build-go build-escript

build-go:
	$(GO_ENV) $(GO) build -o roundtable-http-mcp ./cmd/roundtable-http-mcp

build-escript:
	mise exec -- mix escript.build

test: test-go test-elixir

test-go:
	$(GO_ENV) $(GO) test ./...

test-elixir:
	mise exec -- mix test

clean:
	rm -f roundtable-http-mcp roundtable-cli

release: build
	mkdir -p release
	cp roundtable-http-mcp release/
	cp roundtable-cli release/roundtable
	chmod +x release/roundtable-http-mcp release/roundtable
	@echo "Release artifacts in release/:"
	@ls -la release/roundtable-http-mcp release/roundtable release/roles release/SKILL.md 2>/dev/null || true
