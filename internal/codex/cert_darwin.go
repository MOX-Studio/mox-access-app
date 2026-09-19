//go:build darwin

package codex

import (
	"os"
	"os/exec"
	"path/filepath"
)

func loginKeychain() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Keychains", "login.keychain-db")
}

// CertTrusted asks the system whether the gateway leaf is already trusted for SSL on localhost: a certificate trusted
// once asks nothing again.
func CertTrusted(pemPath string) bool {
	return exec.Command("security", "verify-cert", "-c", pemPath, "-p", "ssl", "-n", "localhost", "-L", "-q").Run() == nil
}

// TrustCert adds the leaf to the login keychain with SSL trust. The engine trusts it through CODEX_CA_CERTIFICATE; the
// Electron shell only through the keychain, and changing trust settings is what macOS asks the user's password for.
func TrustCert(pemPath string) error {
	if CertTrusted(pemPath) {
		return nil
	}
	return run("security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", loginKeychain(), pemPath)
}

// UntrustCert removes the certificate and its trust settings by SHA-1; a certificate that is not there is not an error.
func UntrustCert(sha1 string) error {
	err := run("security", "delete-certificate", "-t", "-Z", sha1, loginKeychain())
	if err != nil && exec.Command("security", "find-certificate", "-Z", loginKeychain()).Run() == nil {
		return nil
	}
	return err
}
