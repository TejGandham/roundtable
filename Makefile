GO = mise exec go@1.26.2 -- go
GO_ENV = GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache
VERSION ?= 0.7.0

.PHONY: all build test vet clean release run run-stdio

all: build

build:
	$(GO_ENV) $(GO) build -o roundtable-http-mcp ./cmd/roundtable-http-mcp

test:
	$(GO_ENV) $(GO) test ./... -count=1 -timeout 60s

vet:
	$(GO_ENV) $(GO) vet ./...

clean:
	rm -f roundtable-http-mcp

run: build
	./roundtable-http-mcp

run-stdio: build
	./roundtable-http-mcp stdio

release: build
	mkdir -p release
	cp roundtable-http-mcp release/
	chmod +x release/roundtable-http-mcp
	@echo "Release artifacts in release/:"
	@ls -la release/ 2>/dev/null || true
