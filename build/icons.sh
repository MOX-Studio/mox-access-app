#!/usr/bin/env bash
# Regenerates the tray icons and AppIcon.icns from build/icon/*.svg (needs rsvg-convert and iconutil; macOS).
set -euo pipefail
cd "$(dirname "$0")/.."
# Tray: systray draws the image at 16×16 pt, so 32 px is the Retina source. Template = black on alpha, macOS recolors it.
for state in "" "-on" "-attention"; do
  sed 's/#8ADDD4/#000000/' "build/icon/mark$state.svg" | rsvg-convert -w 32 -h 32 -o "internal/ui/icon_template$state.png"
  rsvg-convert -w 32 -h 32 "build/icon/mark$state.svg" -o "internal/ui/icon$state.png"
done
# Windows tray wants .ico: 16 and 32 px of the colored mark in one file (PIL packs them).
tmp="$(mktemp -d)"
for state in "" "-on" "-attention"; do
  rsvg-convert -w 16 -h 16 "build/icon/mark$state.svg" -o "$tmp/m16$state.png"
  rsvg-convert -w 32 -h 32 "build/icon/mark$state.svg" -o "$tmp/m32$state.png"
  python3 -c "
from PIL import Image; import sys
a=Image.open(sys.argv[1]); b=Image.open(sys.argv[2])
b.save(sys.argv[3], format='ICO', sizes=[(32,32),(16,16)], append_images=[a], bitmap_format='bmp')" "$tmp/m16$state.png" "$tmp/m32$state.png" "internal/ui/icon$state.ico"
done
for s in 16 32 48 256; do rsvg-convert -w "$s" -h "$s" build/icon/app.svg -o "$tmp/a$s.png"; done
python3 -c "
from PIL import Image; import sys
imgs=[Image.open(p) for p in sys.argv[1:]]
imgs[-1].save('build/icon/app.ico', format='ICO', sizes=[(256,256),(48,48),(32,32),(16,16)], append_images=imgs[:-1], bitmap_format='bmp')" "$tmp/a16.png" "$tmp/a32.png" "$tmp/a48.png" "$tmp/a256.png"
set="$(mktemp -d)/AppIcon.iconset"; mkdir -p "$set"
for s in 16 32 128 256 512; do
  rsvg-convert -w "$s" -h "$s" build/icon/app.svg -o "$set/icon_${s}x${s}.png"
  rsvg-convert -w "$((s*2))" -h "$((s*2))" build/icon/app.svg -o "$set/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$set" -o build/AppIcon.icns
echo "icons: internal/ui/icon*.png, internal/ui/icon*.ico, build/AppIcon.icns, build/icon/app.ico"
# Windows: the exe icon travels as a resource object the Go linker picks up (rsrc: go install github.com/akavel/rsrc@latest).
if command -v rsrc >/dev/null; then
  for arch in amd64 arm64; do rsrc -ico build/icon/app.ico -arch "$arch" -o "cmd/moxaccess/rsrc_windows_$arch.syso"; done
  echo "exe icon: cmd/moxaccess/rsrc_windows_*.syso"
fi
