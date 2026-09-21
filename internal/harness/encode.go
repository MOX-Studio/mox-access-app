package harness

import (
	"encoding/base64"
	"unicode/utf16"
)

// encodeUTF16Base64 is the -EncodedCommand form of a PowerShell script.
func encodeUTF16Base64(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
