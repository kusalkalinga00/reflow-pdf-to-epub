# syntax=docker/dockerfile:1

# Reflow needs CGO (PDFium) and shells out to Tesseract and EPUBCheck, so the
# image is built for its target platform and the runtime carries those tools.
FROM golang:1.27-bookworm AS build

ARG VERSION=dev

RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		build-essential \
		ca-certificates \
		curl \
		pkg-config \
		unzip \
	&& rm -rf /var/lib/apt/lists/*

WORKDIR /src

# pdfium.sh writes the pkg-config file; the Go binding finds the library
# through it, exactly as the Makefile sets it up for local builds.
ENV PKG_CONFIG_PATH=/src/.pdfium/lib/pkgconfig

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Both downloads are pinned by the scripts, so the image does not drift.
# EPUBCheck is copied to a stable path here so the runtime stage can take it
# in one layer instead of copying the versioned directory and renaming it.
RUN ./scripts/pdfium.sh \
	&& ./scripts/epubcheck.sh \
	&& mkdir -p /out/epubcheck \
	&& cp -R "$(dirname "$(find .epubcheck -name epubcheck.jar | head -1)")/." /out/epubcheck/ \
	&& CGO_ENABLED=1 go build -trimpath \
		-ldflags "-s -w -X github.com/kusalkalinga00/tool-factory-epub-convertor/internal/version.Version=${VERSION}" \
		-o /out/reflow ./cmd/reflow

FROM debian:bookworm-slim

# Tesseract reads scanned pages; the JRE runs EPUBCheck, which every
# conversion is validated against before it is handed over.
RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		ca-certificates \
		curl \
		default-jre-headless \
		tesseract-ocr \
		tesseract-ocr-eng \
	&& rm -rf /var/lib/apt/lists/*

# PDFium is a shared library. /usr/local/lib is on Debian's loader path, so
# ldconfig is enough; the build-time rpath points at a directory that does
# not exist here and is skipped by the loader.
COPY --from=build /src/.pdfium/lib/ /usr/local/lib/
RUN ldconfig

# Keep EPUBCheck's layout: its jar needs the lib/ directory beside it.
COPY --from=build /out/epubcheck /opt/epubcheck

ENV EPUBCHECK_JAR=/opt/epubcheck/epubcheck.jar

COPY --from=build /out/reflow /usr/local/bin/reflow

RUN useradd --system --uid 10001 --home-dir /home/reflow --create-home reflow
USER reflow
WORKDIR /home/reflow

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
	CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["reflow"]
CMD ["serve", "-addr", "0.0.0.0:8080"]
