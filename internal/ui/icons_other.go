//go:build !darwin

package ui

import _ "embed"

// Windows wants .ico for the tray; the same mint ring, colored, both as "template" and regular.
var (
	//go:embed icon.ico
	iconOff []byte
	//go:embed icon-on.ico
	iconOn []byte
	//go:embed icon-attention.ico
	iconAttention []byte
)

var iconOffColor, iconOnColor, iconAttentionColor = iconOff, iconOn, iconAttention
