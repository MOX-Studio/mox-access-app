package codex

import (
	"regexp"
	"strings"
)

// The root keys the gateway login manages (codex-login.mjs `managed`): they belong above the first section.
var managedKeyRe = regexp.MustCompile(`^[ \t]*(model_provider|chatgpt_base_url|openai_base_url|cli_auth_credentials_store|experimental_realtime_ws_base_url|experimental_realtime_webrtc_call_base_url)[ \t]*=`)
var sectionRe = regexp.MustCompile(`^[ \t]*\[`)
var featuresRe = regexp.MustCompile(`^[ \t]*\[[ \t]*features[ \t]*\][ \t]*(#.*)?$`)
var moxProviderRe = regexp.MustCompile(`^[ \t]*\[model_providers\.mox[\].]`)
var appsRe = regexp.MustCompile(`^[ \t]*apps[ \t]*=`)

const appsLine = "apps = false # MOX ACCESS"

// ApplyConfig is the awk program of the shell installers in Go: with install=true it puts the MOX fragment first and
// `apps = false # MOX ACCESS` into [features] (ChatGPT Apps cannot authenticate through the gateway); with install=false
// it removes exactly those lines. Everything else in config.toml — user keys, other sections, blank lines — stays;
// a [features] header the removal leaves empty is dropped, one with other keys remains. Running it twice changes nothing.
func ApplyConfig(text, fragment string, install bool) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	var out []string
	top, drop, features, started := true, false, false, false
	section := ""
	blanks, hblanks := 0, 0
	pending := ""
	flush := func() {
		if started {
			for ; blanks > 0; blanks-- {
				out = append(out, "")
			}
		}
		blanks = 0
		started = true
	}
	header := func() {
		if pending == "" {
			return
		}
		for ; hblanks > 0; hblanks-- {
			out = append(out, "")
		}
		started = true
		out = append(out, pending)
		pending = ""
	}
	for _, line := range lines {
		if sectionRe.MatchString(line) {
			top = false
			drop = moxProviderRe.MatchString(line)
			if drop {
				continue
			}
			pending = ""
			if featuresRe.MatchString(line) {
				section = "features"
			} else {
				section = ""
			}
			if section == "features" && !install {
				hblanks, blanks, pending = blanks, 0, line
				continue
			}
			flush()
			out = append(out, line)
			if section == "features" {
				out = append(out, appsLine)
				features = true
			}
			continue
		}
		if drop {
			continue
		}
		if line == "# MOX ACCESS" {
			continue
		}
		if top && managedKeyRe.MatchString(line) {
			continue
		}
		if section == "features" && appsRe.MatchString(line) && (install || strings.HasSuffix(strings.TrimRight(line, " \t"), "# MOX ACCESS")) {
			continue
		}
		if strings.TrimSpace(line) == "" {
			blanks++
			continue
		}
		header()
		flush()
		out = append(out, line)
	}
	if install && !features {
		flush()
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "[features]", appsLine)
	}
	body := strings.Join(out, "\n")
	if install {
		return strings.TrimRight(fragment, "\n") + "\n" + body + "\n"
	}
	if body == "" {
		return ""
	}
	return body + "\n"
}
