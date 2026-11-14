.PHONY: all build test vet fmt fmt-check tidy check run clean

BIN := reflow
PKG := github.com/kusalkalinga00/tool-factory-epub-convertor

all: check build

## build: compile the CLI into bin/
build:
	go build -trimpath -ldflags "-X $(PKG)/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/$(BIN) ./cmd/reflow

## test: run the suite with the race detector
test:
	go test -race -count=1 ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: rewrite files with gofmt
fmt:
	gofmt -w .

## fmt-check: fail if any file needs formatting
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## tidy: verify go.mod and go.sum need no changes
tidy:
	go mod tidy -diff

## check: everything CI runs
check: fmt-check tidy vet test

## run: run the CLI from source
run:
	go run ./cmd/reflow

## clean: remove build output
clean:
	rm -rf bin
