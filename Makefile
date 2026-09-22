# The system Go install is unreliable on this machine, so builds pin the
# toolchain that Go itself downloaded into the module cache.
GOROOT := $(HOME)/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.8.darwin-amd64
GO     := $(GOROOT)/bin/go

.PHONY: build test fmt run tidy

build:
	$(GO) build -o bin/cowrite ./cmd/cowrite

test:
	$(GO) test ./...

fmt:
	$(GOROOT)/bin/gofmt -w cmd internal

run: build
	./bin/cowrite

tidy:
	GOFLAGS=-mod=mod $(GO) mod tidy
