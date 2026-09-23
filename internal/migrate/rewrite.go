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
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

type Stats struct {
	FilesChanged int
	ThreadsLeft  int
}

// RewritePaths replaces the server home with the local one, longest prefix first: ~/.codex of the server becomes
// codexDir (the directory being rewritten), the server's ~/Claud/projects maps to ~/AI/Project by owner, the rest of the
// home maps to newHome. Text files are rewritten byte-wise (encrypted_content is base64 and never matches);
// state_5.sqlite through UPDATE; thread_history_1.sqlite holds byte offsets into the rollouts and is dropped —
// the engine rebuilds it lazily.
func RewritePaths(codexDir, oldHome, newHome string, repos ...Repo) (Stats, error) {
	abs, err := filepath.Abs(codexDir)
	if err != nil {
		return Stats{}, err
	}
	pairs := [][2]string{{oldHome + "/.codex", abs}}
	for _, r := range repos {
		if !repoNameRe.MatchString(r.Name) || r.Name == "." || r.Name == ".." {
			continue
		}
		pairs = append(pairs, [2]string{oldHome + "/Claud/projects/" + r.Name,
			filepath.Join(newHome, "AI", "Project", repoCategory(r), r.Name)})
	}
	pairs = append(pairs, [2]string{oldHome + "/Claud/projects", filepath.Join(newHome, "AI", "Project", "Personal")}, [2]string{oldHome, newHome})
	return rewriteMappings(abs, pairs)
}

// RewriteLocalPaths updates Codex's saved references after the legacy local repositories have moved.
func RewriteLocalPaths(codexDir string, moves [][2]string) (Stats, error) {
	return rewriteMappings(codexDir, moves)
}

func rewriteMappings(codexDir string, pairs [][2]string) (Stats, error) {
	var st Stats
	abs, err := filepath.Abs(codexDir)
	if err != nil {
		return st, err
	}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i][0]) > len(pairs[j][0]) })
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
		out := rewriteTextPaths(data, pairs)
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
		for _, pr := range databasePairs(pairs) {
			for _, column := range []string{"cwd", "rollout_path"} {
				query := "UPDATE threads SET " + column + " = ? || substr(" + column + ", length(?) + 1) WHERE substr(" + column + ", 1, length(?)) = ? AND (length(" + column + ") = length(?) OR substr(" + column + ", length(?) + 1, 1) IN ('/', char(92)))"
				if _, err := db.Exec(query, pr[1], pr[0], pr[0], pr[0], pr[0], pr[0]); err != nil {
					return st, fmt.Errorf("state_5.sqlite: %w", err)
				}
			}
		}
		for _, pr := range databasePairs(pairs) {
			var left int
			if err := db.QueryRow(`SELECT count(*) FROM threads WHERE cwd = ? OR substr(cwd, 1, length(?) + 1) IN (?, ?) OR rollout_path = ? OR substr(rollout_path, 1, length(?) + 1) IN (?, ?)`, pr[0], pr[0], pr[0]+"/", pr[0]+"\x5c", pr[0], pr[0], pr[0]+"/", pr[0]+"\x5c").Scan(&left); err != nil {
				return st, err
			}
			st.ThreadsLeft += left
		}
	}
	for _, name := range []string{"thread_history_1.sqlite", "thread_history_1.sqlite-wal", "thread_history_1.sqlite-shm"} {
		if err := os.Remove(filepath.Join(abs, name)); err != nil && !os.IsNotExist(err) {
			return st, err
		}
	}
	return st, nil
}

func databasePairs(pairs [][2]string) [][2]string {
	out := append([][2]string(nil), pairs...)
	for _, pr := range pairs {
		if strings.Contains(pr[0], `\`) {
			out = append(out, [2]string{strings.ReplaceAll(pr[0], `\`, `/`), strings.ReplaceAll(pr[1], `\`, `/`)})
		}
	}
	return out
}

func rewriteTextPaths(data []byte, pairs [][2]string) []byte {
	out := data
	for _, pr := range pairs {
		variants := [][2]string{pr}
		if strings.Contains(pr[0], `\`) {
			variants = append(variants, [2]string{strings.ReplaceAll(pr[0], `\`, `\\`), strings.ReplaceAll(pr[1], `\`, `\\`)})
			variants = append(variants, [2]string{strings.ReplaceAll(pr[0], `\`, `/`), strings.ReplaceAll(pr[1], `\`, `/`)})
		}
		for _, variant := range variants {
			out = replacePathBytes(out, []byte(variant[0]), []byte(variant[1]))
		}
	}
	return out
}

func replacePathBytes(data, old, replacement []byte) []byte {
	var out []byte
	for len(data) > 0 {
		at := bytes.Index(data, old)
		if at < 0 {
			return append(out, data...)
		}
		end := at + len(old)
		if end == len(data) || pathBoundary(data[end]) {
			out = append(out, data[:at]...)
			out = append(out, replacement...)
		} else {
			out = append(out, data[:end]...)
		}
		data = data[end:]
	}
	return out
}

func pathBoundary(ch byte) bool {
	switch ch {
	case '/', '\\', '"', '\'', ',', ']', '}', ' ', '\t', '\r', '\n':
		return true
	}
	return false
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
