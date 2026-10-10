package codex

import (
	"regexp"
	"strings"
)

// Secret is a personal service key the gateway grants to the employee (GET /mox/secrets): an MCP server reached over
// HTTP with the key as a bearer header. Version moves whenever the name, the address or the value changes.
type Secret struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Value   string `json:"value"`
	Version int    `json:"version"`
}

const secretMark = "# MOX ACCESS secret"

// The same checks the gateway makes before it stores a secret: the name becomes a TOML table key and the value a
// basic TOML string written without escaping, so anything else is dropped rather than written.
var (
	secretNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)
	secretURLRe   = regexp.MustCompile(`^https://[^\s"'\\]{3,500}$`)
	secretValueRe = regexp.MustCompile(`^[\x21-\x7e]+$`) // 16…4096 characters, checked apart: RE2 counts to 1000
	tableRe       = regexp.MustCompile(`^[ \t]*\[`)
)

func (s Secret) writable() bool {
	return s.Kind == "mcp_http" && s.Name != "mox" && secretNameRe.MatchString(s.Name) && secretURLRe.MatchString(s.URL) &&
		len(s.Value) >= 16 && len(s.Value) <= 4096 && secretValueRe.MatchString(s.Value) && !strings.ContainsAny(s.Value, `"'\`)
}

// WithoutSecrets is config.toml with the blocks of ApplySecrets taken out: what a backup may keep.
func WithoutSecrets(text string) string { out, _ := ApplySecrets(text, nil); return out }

// ApplySecrets rewrites the [mcp_servers.<name>] tables marked as MOX ACCESS secrets so they match secrets exactly:
// the marked tables are removed wherever they are and the current ones appended at the end. Every other line stays.
// A server of that name the employee configured herself is left alone and its name returned in skipped, as is a
// secret that cannot be written safely. Running it twice changes nothing.
func ApplySecrets(text string, secrets []Secret) (string, []string) {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
	}
	var kept []string
	own := map[string]bool{}
	drop := false
	for _, line := range lines {
		if tableRe.MatchString(line) {
			drop = strings.HasSuffix(strings.TrimRight(line, " \t"), secretMark)
			if !drop {
				if name, ok := strings.CutPrefix(strings.TrimSpace(line), "[mcp_servers."); ok {
					own[strings.SplitN(strings.TrimRight(name, "]"), ".", 2)[0]] = true
				}
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	var skipped []string
	for _, s := range secrets {
		if !s.writable() || own[s.Name] {
			skipped = append(skipped, s.Name)
			continue
		}
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(kept, "[mcp_servers."+s.Name+"] "+secretMark, `url = "`+s.URL+`"`, `http_headers = { Authorization = "Bearer `+s.Value+`" }`)
	}
	if len(kept) == 0 {
		return "", skipped
	}
	return strings.Join(kept, "\n") + "\n", skipped
}
