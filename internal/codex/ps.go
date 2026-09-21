package codex

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PSQuote wraps a value in PowerShell single quotes; a quote inside becomes ”. Values from the bundle (URLs, paths)
// travel into scripts only this way.
func PSQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Thumbprint is the SHA-1 of the certificate's DER, upper-case hex: the form Cert:\CurrentUser\Root lists and the
// panel shows. It is how a Windows store is asked "is this certificate already trusted?".
func Thumbprint(pemPath string) (string, error) {
	raw, err := os.ReadFile(pemPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("сертификат шлюза не читается")
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

// EnvScript is the PowerShell that sets (or, with nil values, removes) user environment variables, exactly as the
// installer of 2026-09-18 did: [Environment]::SetEnvironmentVariable writes the registry and broadcasts the change.
func EnvScript(vars map[string]string, keys []string, remove bool) string {
	var b strings.Builder
	for _, k := range keys {
		if remove {
			fmt.Fprintf(&b, "[Environment]::SetEnvironmentVariable(%s, $null, 'User')\n", PSQuote(k))
		} else {
			fmt.Fprintf(&b, "[Environment]::SetEnvironmentVariable(%s, %s, 'User')\n", PSQuote(k), PSQuote(vars[k]))
		}
	}
	return b.String()
}
