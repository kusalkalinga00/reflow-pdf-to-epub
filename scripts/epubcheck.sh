#!/usr/bin/env bash
# Downloads a pinned EPUBCheck release and unpacks it.
#
# EPUBCheck needs a JVM; the jar path is exported by the Makefile as
# EPUBCHECK_JAR so the validator can find it without a PATH dance.
set -euo pipefail

dir="${EPUBCHECK_DIR:-$PWD/.epubcheck}"
version="${EPUBCHECK_VERSION:-5.4.0}"
jar="$dir/epubcheck-$version/epubcheck.jar"

if [ -f "$jar" ]; then
	echo "epubcheck: already installed in $dir"
	exit 0
fi

mkdir -p "$dir"
url="https://github.com/w3c/epubcheck/releases/download/v$version/epubcheck-$version.zip"

echo "epubcheck: downloading $url"
curl -fsSL -o "$dir/epubcheck.zip" "$url"
unzip -q -o "$dir/epubcheck.zip" -d "$dir"
rm -f "$dir/epubcheck.zip"

echo "epubcheck: installed $version in $dir"
