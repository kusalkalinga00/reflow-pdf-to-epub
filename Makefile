.PHONY: all build test vet fmt fmt-check tidy check run clean deps

BIN := reflow
PKG := github.com/kusalkalinga00/tool-factory-epub-convertor

# PDFium is discovered through pkg-config; scripts/pdfium.sh installs it into
# $(PDFIUM_DIR) and writes the .pc file that points at it.
PDFIUM_DIR ?= $(CURDIR)/.pdfium
PDFIUM_PC := $(PDFIUM_DIR)/lib/pkgconfig/pdfium.pc
export PKG_CONFIG_PATH := $(PDFIUM_PC:/pdfium.pc=)

EPUBCHECK_DIR ?= $(CURDIR)/.epubcheck
EPUBCHECK_VERSION ?= 5.4.0
EPUBCHECK_JAR := $(EPUBCHECK_DIR)/epubcheck-$(EPUBCHECK_VERSION)/epubcheck.jar
export EPUBCHECK_JAR

all: check build

## deps: download the pinned PDFium SDK if it is not already present
deps: $(PDFIUM_PC) $(EPUBCHECK_JAR)

$(PDFIUM_PC):
	PDFIUM_DIR=$(PDFIUM_DIR) ./scripts/pdfium.sh

$(EPUBCHECK_JAR):
	EPUBCHECK_DIR=$(EPUBCHECK_DIR) EPUBCHECK_VERSION=$(EPUBCHECK_VERSION) ./scripts/epubcheck.sh

## build: compile the CLI into bin/
build: deps
	go build -trimpath -ldflags "-X $(PKG)/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/$(BIN) ./cmd/reflow

## test: run the suite with the race detector
test: deps
	go test -race -count=1 ./...

## vet: run go vet
vet: deps
	go vet ./...

## fmt: rewrite files with gofmt
fmt:
	gofmt -w .

## fmt-check: fail if any file needs formatting
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## tidy: verify go.mod and go.sum need no changes
tidy: deps
	go mod tidy -diff

## check: everything CI runs
check: deps fmt-check tidy vet test

## run: run the CLI from source
run: deps
	go run ./cmd/reflow

## clean: remove build output, keeping the PDFium download
clean:
	rm -rf bin

## clean-all: remove build output and downloaded SDKs
clean-all: clean
	rm -rf $(PDFIUM_DIR) $(EPUBCHECK_DIR)
