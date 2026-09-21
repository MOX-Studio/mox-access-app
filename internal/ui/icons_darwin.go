//go:build darwin

package ui

import _ "embed"

// macOS draws template PNGs (black on alpha) in the menu bar's own color; the colored ones are the fallback systray keeps.
var (
	//go:embed icon_template.png
	iconOff []byte
	//go:embed icon.png
	iconOffColor []byte
	//go:embed icon_template-on.png
	iconOn []byte
	//go:embed icon-on.png
	iconOnColor []byte
	//go:embed icon_template-attention.png
	iconAttention []byte
	//go:embed icon-attention.png
	iconAttentionColor []byte
)
