#!/usr/bin/env bash
# Regenerates the tray icons and AppIcon.icns from build/icon/*.svg (needs rsvg-convert and iconutil; macOS).
set -euo pipefail
cd "$(dirname "$0")/.."
# Tray: systray draws the image at 16×16 pt, so 32 px is the Retina source. Template = black on alpha, macOS recolors it.
for state in "" "-on" "-attention"; do
  sed 's/#8ADDD4/#000000/' "build/icon/mark$state.svg" | rsvg-convert -w 32 -h 32 -o "internal/ui/icon_template$state.png"
  rsvg-convert -w 32 -h 32 "build/icon/mark$state.svg" -o "internal/ui/icon$state.png"
done
set="$(mktemp -d)/AppIcon.iconset"; mkdir -p "$set"
for s in 16 32 128 256 512; do
  rsvg-convert -w "$s" -h "$s" build/icon/app.svg -o "$set/icon_${s}x${s}.png"
  rsvg-convert -w "$((s*2))" -h "$((s*2))" build/icon/app.svg -o "$set/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$set" -o build/AppIcon.icns
echo "icons: internal/ui/icon*.png, build/AppIcon.icns"
