# The system Go install is unreliable on this machine, so builds pin the
# toolchain Go itself downloaded into the module cache.
GOROOT := $(HOME)/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.8.darwin-amd64
GO     := $(GOROOT)/bin/go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fmt run tidy vet release clean

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/cowrite ./cmd/cowrite

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GOROOT)/bin/gofmt -w cmd internal

run: build
	./bin/cowrite

tidy:
	GOFLAGS=-mod=mod $(GO) mod tidy

# Cross-compiles for the platforms people actually run this on. CGO is off
# throughout (modernc sqlite is pure Go), so these need no C toolchain.
release:
	@mkdir -p dist
	@for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build -ldflags "$(LDFLAGS)" \
			-o dist/cowrite-$$os-$$arch$$ext ./cmd/cowrite || exit 1; \
	done
	@echo "built:" && ls -lh dist/

clean:
	rm -rf bin dist
