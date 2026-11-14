#!/usr/bin/env bash
# Downloads a pinned PDFium build and writes a pkg-config file for it.
#
# The Go binding discovers PDFium through pkg-config, so this script produces
# both the shared library and the .pc file that points at it. It is idempotent:
# running it again with the library already present does nothing.
set -euo pipefail

dir="${PDFIUM_DIR:-$PWD/.pdfium}"
tag="${PDFIUM_TAG:-chromium/8152}"
version="${PDFIUM_VERSION:-159.0.8152.0}"

if [ -f "$dir/lib/pkgconfig/pdfium.pc" ] && [ -f "$dir/VERSION" ]; then
	echo "pdfium: already installed in $dir"
	exit 0
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
darwin) os=mac ;;
linux) os=linux ;;
*)
	echo "pdfium: unsupported OS: $os" >&2
	exit 1
	;;
esac

arch="$(uname -m)"
case "$arch" in
arm64 | aarch64) arch=arm64 ;;
x86_64 | amd64) arch=x64 ;;
*)
	echo "pdfium: unsupported architecture: $arch" >&2
	exit 1
	;;
esac

asset="pdfium-$os-$arch.tgz"
url="https://github.com/bblanchon/pdfium-binaries/releases/download/$tag/$asset"

mkdir -p "$dir"
echo "pdfium: downloading $url"
curl -fsSL -o "$dir/pdfium.tgz" "$url"
tar xzf "$dir/pdfium.tgz" -C "$dir"
rm -f "$dir/pdfium.tgz"

mkdir -p "$dir/lib/pkgconfig"
# rpath is baked in so binaries and test suites run without DYLD_LIBRARY_PATH.
cat >"$dir/lib/pkgconfig/pdfium.pc" <<EOF
prefix=$dir
libdir=\${prefix}/lib
includedir=\${prefix}/include

Name: pdfium
Description: PDFium
Version: $version
Libs: -L\${libdir} -Wl,-rpath,\${libdir} -lpdfium
Cflags: -I\${includedir}
EOF

echo "pdfium: installed $version in $dir"
