#!/usr/bin/env bash
# Builds "MOX Access.exe" for Windows (amd64 by default; ARCH=arm64 for Windows on ARM) and zips it into dist/.
# Pure Go, so it cross-compiles from any machine. Usage: build/windows.sh [version]
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo dev)}"; V="${V#v}"
ARCH="${ARCH:-amd64}"
mkdir -p dist/windows-"$ARCH"
CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -H windowsgui -X main.Version=$V" -o "dist/windows-$ARCH/MOX Access.exe" ./cmd/moxaccess
( cd "dist/windows-$ARCH" && rm -f "../MOX-Access-windows-$ARCH.zip" && zip -qr "../MOX-Access-windows-$ARCH.zip" "MOX Access.exe" )
echo "built: dist/windows-$ARCH/MOX Access.exe ($V, $ARCH) → dist/MOX-Access-windows-$ARCH.zip ($(du -h "dist/MOX-Access-windows-$ARCH.zip" | cut -f1))"
