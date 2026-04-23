GO = mise exec go@1.26.2 -- go
GO_ENV = GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache
VERSION ?= 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: all build test vet clean release release-linux release-darwin release-windows run

all: build

build:
	$(GO_ENV) $(GO) build -ldflags "$(LDFLAGS)" -o roundtable ./cmd/roundtable

test:
	$(GO_ENV) $(GO) test ./... -count=1 -timeout 60s

vet:
	$(GO_ENV) $(GO) vet ./...

clean:
	rm -f roundtable roundtable.exe
	rm -f release/roundtable-*

run: build
	./roundtable stdio

# Cross-compile targets. Each writes one binary to release/ with an arch suffix.
# Windows binaries get a .exe extension; all others are bare executables.
release: release-linux release-darwin release-windows
	chmod +x release/roundtable-linux-amd64 release/roundtable-linux-arm64 release/roundtable-darwin-amd64 release/roundtable-darwin-arm64 2>/dev/null || true
	@echo "Release artifacts in release/:"
	@ls -la release/ 2>/dev/null || true

release-linux:
	mkdir -p release
	$(GO_ENV) GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o release/roundtable-linux-amd64 ./cmd/roundtable
	$(GO_ENV) GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o release/roundtable-linux-arm64 ./cmd/roundtable

release-darwin:
	mkdir -p release
	$(GO_ENV) GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o release/roundtable-darwin-amd64 ./cmd/roundtable
	$(GO_ENV) GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o release/roundtable-darwin-arm64 ./cmd/roundtable

release-windows:
	mkdir -p release
	$(GO_ENV) GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o release/roundtable-windows-amd64.exe ./cmd/roundtable
