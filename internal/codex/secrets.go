package codex

import (
	"regexp"
	"strings"
)

// Secret is a personal service key the gateway grants to the employee (GET /mox/secrets): kind mcp_http is an MCP server
// reached over HTTP with the key as a bearer header, kind env an environment variable of the agent's shell commands
// (no address). Version moves whenever the name, the address or the value changes.
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
	envNameRe     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	secretURLRe   = regexp.MustCompile(`^https://[^\s"'\\]{3,500}$`)
	secretValueRe = regexp.MustCompile(`^[\x21-\x7e]+$`) // 16…4096 characters, checked apart: RE2 counts to 1000
	tableRe       = regexp.MustCompile(`^[ \t]*\[`)
	keyRe         = regexp.MustCompile(`^[ \t]*"?([A-Za-z0-9_-]+)"?[ \t]*([=.])`)
	envMarkedRe   = regexp.MustCompile(`^[ \t]*[A-Z][A-Z0-9_]*[ \t]*=.*` + secretMark + `[ \t]*$`)
)

const envTable = "shell_environment_policy.set"

func (s Secret) writable() bool {
	if len(s.Value) < 16 || len(s.Value) > 4096 || !secretValueRe.MatchString(s.Value) || strings.ContainsAny(s.Value, `"'\`) {
		return false
	}
	switch s.Kind {
	case "mcp_http":
		return s.Name != "mox" && secretNameRe.MatchString(s.Name) && secretURLRe.MatchString(s.URL)
	case "env": // a variable may not take over the paths or the channel to MOX itself
		return envNameRe.MatchString(s.Name) && s.Name != "PATH" && s.Name != "HOME" &&
			!strings.HasPrefix(s.Name, "CODEX_") && !strings.HasPrefix(s.Name, "OPENAI_")
	}
	return false
}

// tableName is the name of a [table] header without spaces or quotes, "" for an [[array]] header.
func tableName(line string) string {
	line = strings.TrimSpace(line)
	end := strings.Index(line, "]")
	if strings.HasPrefix(line, "[[") || end < 0 {
		return ""
	}
	return strings.NewReplacer(" ", "", "\t", "", `"`, "").Replace(line[1:end])
}

// WithoutSecrets is config.toml with the blocks of ApplySecrets taken out: what a backup may keep.
func WithoutSecrets(text string) string { out, _ := ApplySecrets(text, nil); return out }

// ApplySecrets rewrites the parts of config.toml marked as MOX ACCESS secrets so they match secrets exactly: the marked
// [mcp_servers.<name>] tables and the marked variable lines of [shell_environment_policy.set] are removed wherever they
// are and the current ones written again. Every other line stays. A variable goes into the employee's own
// [shell_environment_policy.set] table when she has one (TOML allows the table once), else into a marked table at the
// end. A server or variable of that name the employee configured herself is left alone and its name returned in
// skipped, as is a secret that cannot be written safely and a variable when her set cannot take another line
// (set = { … } inline or a dotted key). Running it twice changes nothing.
func ApplySecrets(text string, secrets []Secret) (string, []string) {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
	}
	var kept []string
	own := map[string]bool{}
	var ownVars []string
	setAt, setBlocked := -1, false
	drop, table := false, ""
	for _, line := range lines {
		if tableRe.MatchString(line) {
			drop = strings.HasSuffix(strings.TrimRight(line, " \t"), secretMark)
			if !drop {
				table = tableName(line)
				if name, ok := strings.CutPrefix(table, "mcp_servers."); ok {
					own[strings.SplitN(name, ".", 2)[0]] = true
				}
				if table == envTable && setAt < 0 {
					setAt = len(kept)
				}
			}
		} else if !drop && envMarkedRe.MatchString(line) {
			continue
		} else if m := keyRe.FindStringSubmatch(line); !drop && m != nil {
			switch {
			case table == envTable:
				ownVars = append(ownVars, m[1])
			case table == "" && m[1] == "shell_environment_policy":
				// shell_environment_policy = { … } closes the table; shell_environment_policy.set… defines set by dotted keys
				setBlocked = setBlocked || m[2] == "=" || strings.HasPrefix(strings.TrimSpace(line), "shell_environment_policy.set")
			case table == "shell_environment_policy" && m[1] == "set":
				setBlocked = true
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	var skipped, vars []string
	for _, s := range secrets {
		if !s.writable() || s.Kind == "mcp_http" && own[s.Name] || s.Kind == "env" && (setBlocked || ownVar(ownVars, s.Name)) {
			skipped = append(skipped, s.Name)
			continue
		}
		if s.Kind == "env" {
			vars = append(vars, s.Name+` = "`+s.Value+`" `+secretMark)
			continue
		}
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(kept, "[mcp_servers."+s.Name+"] "+secretMark, `url = "`+s.URL+`"`, `http_headers = { Authorization = "Bearer `+s.Value+`" }`)
	}
	if len(vars) > 0 && setAt >= 0 {
		kept = append(kept[:setAt+1], append(vars, kept[setAt+1:]...)...)
	} else if len(vars) > 0 {
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(append(kept, "["+envTable+"] "+secretMark), vars...)
	}
	if len(kept) == 0 {
		return "", skipped
	}
	return strings.Join(kept, "\n") + "\n", skipped
}

// ownVar: Windows treats variable names without case, so her gcore_api_token is the same variable.
func ownVar(names []string, name string) bool {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
