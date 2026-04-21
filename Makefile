GO = mise exec go@1.26.2 -- go
GO_ENV = GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache
VERSION ?= 0.8.0

.PHONY: all build test vet clean release run

all: build

build:
	$(GO_ENV) $(GO) build -ldflags "-s -w -X main.version=$(VERSION)" -o roundtable ./cmd/roundtable

test:
	$(GO_ENV) $(GO) test ./... -count=1 -timeout 60s

vet:
	$(GO_ENV) $(GO) vet ./...

clean:
	rm -f roundtable
	rm -f release/roundtable-*

run: build
	./roundtable stdio

release:
	mkdir -p release
	$(GO_ENV) GOOS=linux GOARCH=amd64 $(GO) build -ldflags "-s -w -X main.version=$(VERSION)" -o release/roundtable-linux-amd64 ./cmd/roundtable
	$(GO_ENV) GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "-s -w -X main.version=$(VERSION)" -o release/roundtable-darwin-arm64 ./cmd/roundtable
	chmod +x release/roundtable-*
	@echo "Release artifacts in release/:"
	@ls -la release/ 2>/dev/null || true
