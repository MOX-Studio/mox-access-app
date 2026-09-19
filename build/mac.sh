#!/usr/bin/env bash
# Builds "MOX Access.app" for macOS (native arch by default; ARCH=amd64|arm64 to pick) and zips it into dist/.
# Usage: build/mac.sh [version]    — version defaults to the last git tag or "dev".
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo dev)}"; V="${V#v}"
ARCH="${ARCH:-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')}"
app="dist/MOX Access.app"; rm -rf "$app"; mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
CGO_ENABLED=1 GOOS=darwin GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -X main.Version=$V" -o "$app/Contents/MacOS/moxaccess" ./cmd/moxaccess
sed "s/__VERSION__/$V/g" build/Info.plist > "$app/Contents/Info.plist"
printf 'APPL????' > "$app/Contents/PkgInfo"
# Ad-hoc signature: without it macOS Gatekeeper refuses to even offer "Open" on some versions. Notarization is later.
codesign --force --deep --sign - "$app" 2>/dev/null || true
( cd dist && rm -f "MOX-Access-mac-$ARCH.zip" && zip -qry "MOX-Access-mac-$ARCH.zip" "MOX Access.app" )
echo "built: $app ($V, $ARCH) → dist/MOX-Access-mac-$ARCH.zip ($(du -h "dist/MOX-Access-mac-$ARCH.zip" | cut -f1))"
