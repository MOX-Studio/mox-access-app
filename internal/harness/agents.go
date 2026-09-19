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
	block := beginMarker + "\nВерсия harness: " + version + " — репо MOX-Studio/mox-harness; не править руками, обновляет установщик.\n\n" + strings.TrimRight(body, "\n") + "\n" + endMarker + "\n"
	existing := ""
	if data, err := os.ReadFile(target); err == nil {
		existing = string(data)
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
	if out == existing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(out), 0o644)
}
