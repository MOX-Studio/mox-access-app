// Package migrate moves an employee's Codex state and projects from vps6 to this machine: download the export the
// gateway holds for their key, unpack it, rewrite every absolute server path, merge the threads into the local
// ~/.codex and clone the repositories. The facts behind each step are in plan 1, «S1 — результат».
package migrate

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Stats struct {
	FilesChanged int
	ThreadsLeft  int
}

// RewritePaths replaces the server home with the local one, longest prefix first: ~/.codex of the server becomes
// codexDir (the directory being rewritten), the server's ~/Claud/projects becomes ~/MOX/projects here, the rest of the
// home maps to newHome. Text files are rewritten byte-wise (encrypted_content is base64 and never matches);
// state_5.sqlite through UPDATE; thread_history_1.sqlite holds byte offsets into the rollouts and is dropped —
// the engine rebuilds it lazily.
func RewritePaths(codexDir, oldHome, newHome string) (Stats, error) {
	var st Stats
	abs, err := filepath.Abs(codexDir)
	if err != nil {
		return st, err
	}
	pairs := [][2]string{{oldHome + "/.codex", abs}, {oldHome + "/Claud/projects", filepath.Join(newHome, "MOX", "projects")}, {oldHome, newHome}}
	var targets []string
	for _, glob := range []string{"sessions/*/*/*/*.jsonl", "archived_sessions/*/*/*/*.jsonl", "memories/*.md", "memories/*/*.md", "config.toml", "session_index.jsonl"} {
		m, _ := filepath.Glob(filepath.Join(abs, glob))
		targets = append(targets, m...)
	}
	for _, p := range targets {
		if strings.Contains(filepath.Base(p), ".bak-") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return st, err
		}
		out := data
		for _, pr := range pairs {
			out = bytes.ReplaceAll(out, []byte(pr[0]), []byte(pr[1]))
		}
		if !bytes.Equal(out, data) {
			info, _ := os.Stat(p)
			if err := os.WriteFile(p, out, info.Mode().Perm()); err != nil {
				return st, err
			}
			st.FilesChanged++
		}
	}
	dbPath := filepath.Join(abs, "state_5.sqlite")
	if _, err := os.Stat(dbPath); err == nil {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return st, err
		}
		defer db.Close()
		for _, pr := range pairs {
			if _, err := db.Exec(`UPDATE threads SET cwd = replace(cwd, ?, ?), rollout_path = replace(rollout_path, ?, ?)`, pr[0], pr[1], pr[0], pr[1]); err != nil {
				return st, fmt.Errorf("state_5.sqlite: %w", err)
			}
		}
		if err := db.QueryRow(`SELECT count(*) FROM threads WHERE cwd LIKE ? OR rollout_path LIKE ?`, oldHome+"%", oldHome+"%").Scan(&st.ThreadsLeft); err != nil {
			return st, err
		}
	}
	for _, name := range []string{"thread_history_1.sqlite", "thread_history_1.sqlite-wal", "thread_history_1.sqlite-shm"} {
		if err := os.Remove(filepath.Join(abs, name)); err != nil && !os.IsNotExist(err) {
			return st, err
		}
	}
	return st, nil
}

// walkFiles lists regular files under root matching a suffix.
func walkFiles(root, suffix string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) {
			out = append(out, p)
		}
		return nil
	})
	return out
}
