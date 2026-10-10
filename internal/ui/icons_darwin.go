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
	// The same three with a dot top right: a newer version waits to be installed.
	//go:embed icon_template-update.png
	iconOffUpdate []byte
	//go:embed icon-update.png
	iconOffUpdateColor []byte
	//go:embed icon_template-on-update.png
	iconOnUpdate []byte
	//go:embed icon-on-update.png
	iconOnUpdateColor []byte
	//go:embed icon_template-attention-update.png
	iconAttentionUpdate []byte
	//go:embed icon-attention-update.png
	iconAttentionUpdateColor []byte
)
