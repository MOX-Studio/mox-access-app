// Package harness connects the machine to MOX-Studio/mox-harness: the plugin marketplace of the team, the block of
// team rules in ~/.codex/AGENTS.md, the employee identity for `end`, and the class-aware git hook in every project.
// It is setup.sh of the harness repository in Go, with the same order of steps.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "<!-- mox-harness:begin -->"
	endMarker   = "<!-- mox-harness:end -->"
)

// ApplyAgentsBlock inserts or refreshes the team block between the markers and leaves everything else in the file —
// the personal notes of the employee — untouched. A marker pair that is broken (one without the other, duplicates,
// end before begin) is an error: the file is left as is for a human.
func ApplyAgentsBlock(target, body, version string) error {
	return ApplyAgentsBlockWithProfile(target, body, version, "")
}

// ApplyAgentsBlockWithProfile installs the personal text once and refreshes only the team block.
// A person can edit their section without a later harness upgrade replacing it.
func ApplyAgentsBlockWithProfile(target, body, version, profile string) error {
	block := beginMarker + "\nВерсия harness: " + version + " — репо MOX-Studio/mox-harness; не править руками, обновляет установщик.\n\n" + strings.TrimRight(body, "\n") + "\n" + endMarker + "\n"
	existing := ""
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s не является обычным файлом — правила не изменены", target)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if data, err := os.ReadFile(target); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	personalBegin := "<!-- mox-personal:begin -->"
	personalEnd := "<!-- mox-personal:end -->"
	if profile != "" && (strings.Count(profile, personalBegin) != 1 || strings.Count(profile, personalEnd) != 1 || strings.Index(profile, personalBegin) > strings.Index(profile, personalEnd)) {
		return fmt.Errorf("личный шаблон MOX повреждён — файл не изменён")
	}
	pb, pe := strings.Count(existing, personalBegin), strings.Count(existing, personalEnd)
	if pb != pe || pb > 1 || (pb == 1 && strings.Index(existing, personalBegin) > strings.Index(existing, personalEnd)) {
		return fmt.Errorf("в %s повреждены маркеры mox-personal — файл не изменён", target)
	}
	b, e := strings.Count(existing, beginMarker), strings.Count(existing, endMarker)
	var out string
	switch {
	case b == 0 && e == 0:
		sep := ""
		if existing != "" {
			if strings.HasSuffix(existing, "\n") {
				sep = "\n"
			} else {
				sep = "\n\n"
			}
		}
		out = existing + sep + block
	case b == 1 && e == 1 && strings.Index(existing, beginMarker) < strings.Index(existing, endMarker):
		start := strings.Index(existing, beginMarker)
		stop := strings.Index(existing, endMarker) + len(endMarker)
		tail := existing[stop:]
		if !strings.HasPrefix(tail, "\n") {
			tail = "\n" + tail
		}
		out = existing[:start] + strings.TrimRight(block, "\n") + tail
	default:
		return fmt.Errorf("в %s повреждены маркеры mox-harness (begin=%d, end=%d) — поправьте файл вручную", target, b, e)
	}
	if profile != "" && pb == 0 {
		out = strings.TrimRight(profile, "\n") + "\n\n" + out
	}
	if out == existing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(out), 0o644)
}

func profileFor(name, root string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	var slug string
	switch strings.ToLower(fields[0]) {
	case "катя", "екатерина", "katya", "katia", "ekaterina":
		slug = "katya"
	case "лиля", "лилия", "lilya", "liliya", "lilia":
		slug = "lilya"
	case "динара", "dinara":
		slug = "dinara"
	default:
		return ""
	}
	return filepath.Join(root, "profiles", slug+".md")
}
