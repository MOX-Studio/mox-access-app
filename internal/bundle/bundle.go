// Package bundle reads the .moxaccess file the MOX ACCESS panel issues to an employee: the same login, config fragment,
// environment, gateway certificate and relay the shell installers carried, as data (server/bundle.mjs, format moxaccess/1).
// It holds the MOX key and the private key of the relay: callers keep it 0600 and never log it.
package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const Format = "moxaccess/1"

type Employee struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Gateway struct {
	Origin    string `json:"origin"`
	LocalPort int    `json:"localPort"`
}

type Config struct {
	Fragment string          `json:"fragment"`
	Features map[string]bool `json:"features"`
}

type TLS struct {
	Certificate string `json:"certificate"`
	SHA1        string `json:"sha1"`
}

type Relay struct {
	User       string `json:"user"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	HostKey    string `json:"hostKey"`
	PrivateKey string `json:"privateKey"`
}

type Bundle struct {
	Format   string          `json:"format"`
	IssuedAt time.Time       `json:"issuedAt"`
	Employee Employee        `json:"employee"`
	Gateway  Gateway         `json:"gateway"`
	Auth     json.RawMessage `json:"auth"`
	Config   Config          `json:"config"`
	Env      [][]*string     `json:"env"`
	TLS      *TLS            `json:"tls"`
	Relay    *Relay          `json:"relay"`
}

var (
	certRe    = regexp.MustCompile(`^-----BEGIN CERTIFICATE-----\n([A-Za-z0-9+/=]+\n)+-----END CERTIFICATE-----\n$`)
	sshKeyRe  = regexp.MustCompile(`^-----BEGIN OPENSSH PRIVATE KEY-----\n([A-Za-z0-9+/=]+\n)+-----END OPENSSH PRIVATE KEY-----\n$`)
	hostKeyRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]+=*$`)
	emailRe   = regexp.MustCompile(`^\S+@\S+\.\S+$`)
)

// Parse decodes and validates a bundle. Anything that would later be written to disk or fed to ssh/security is
// checked for shape here, so a tampered file fails before it touches the machine.
func Parse(raw []byte) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("файл не читается как .moxaccess: %w", err)
	}
	if b.Format != Format {
		return nil, fmt.Errorf("неизвестный формат файла %q (ожидается %s)", b.Format, Format)
	}
	if !emailRe.MatchString(b.Employee.Email) {
		return nil, errors.New("в файле нет почты сотрудника")
	}
	if !strings.HasPrefix(b.Gateway.Origin, "https://") && !strings.HasPrefix(b.Gateway.Origin, "http://") {
		return nil, errors.New("в файле нет адреса шлюза")
	}
	if b.Gateway.LocalPort <= 0 || b.Gateway.LocalPort > 65535 {
		return nil, errors.New("в файле нет порта шлюза")
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(b.Auth, &auth); err != nil || auth.Tokens.AccessToken == "" {
		return nil, errors.New("в файле нет ключа MOX")
	}
	if b.Config.Fragment == "" {
		return nil, errors.New("в файле нет фрагмента конфига Codex")
	}
	for _, pair := range b.Env {
		if len(pair) != 2 || pair[0] == nil || *pair[0] == "" {
			return nil, errors.New("переменные окружения в файле повреждены")
		}
	}
	if b.TLS != nil {
		if !certRe.MatchString(b.TLS.Certificate) {
			return nil, errors.New("сертификат шлюза в файле повреждён")
		}
		if len(b.TLS.SHA1) != 40 {
			return nil, errors.New("отпечаток сертификата в файле повреждён")
		}
	}
	if b.Relay != nil {
		r := b.Relay
		if r.User == "" || r.Host == "" || r.Port <= 0 || r.Port > 65535 {
			return nil, errors.New("данные релея в файле повреждены")
		}
		if !hostKeyRe.MatchString(r.HostKey) {
			return nil, errors.New("host-ключ релея в файле повреждён")
		}
		if !sshKeyRe.MatchString(r.PrivateKey) {
			return nil, errors.New("ключ релея в файле повреждён")
		}
	}
	return &b, nil
}

// AccessToken is the MOX key: the access_token of the issued login.
func (b *Bundle) AccessToken() string {
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	_ = json.Unmarshal(b.Auth, &auth)
	return auth.Tokens.AccessToken
}

// EnvVars is the environment of the Desktop shell: the panel leaves CODEX_CA_CERTIFICATE without a value because the
// application decides where the PEM lives; no_proxy is the lowercase twin of NO_PROXY the engine also reads.
func (b *Bundle) EnvVars(pemPath string) map[string]string {
	env := map[string]string{}
	for _, pair := range b.Env {
		name := *pair[0]
		switch {
		case name == "CODEX_CA_CERTIFICATE":
			if b.TLS != nil && pemPath != "" {
				env[name] = pemPath
			}
		case pair[1] != nil:
			env[name] = *pair[1]
		}
	}
	if v, ok := env["NO_PROXY"]; ok {
		env["no_proxy"] = v
	}
	return env
}
