package migrate

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MergeThreads brings the rewritten server state into the local ~/.codex: rollouts are copied into the same
// sessions/YYYY/MM/DD/ layout (thread ids never collide), rows of `threads` are inserted by the columns both
// databases share — the two sides may sit on different migration versions — with rollout_path pointing at the
// local home. A local home without state_5.sqlite gets the file whole. Returns the number of rows added.
func MergeThreads(src, dst string) (int, error) {
	srcAbs, _ := filepath.Abs(src)
	dstAbs, _ := filepath.Abs(dst)
	for _, sub := range []string{"sessions", "archived_sessions"} {
		for _, p := range walkFiles(filepath.Join(srcAbs, sub), ".jsonl") {
			rel, _ := filepath.Rel(srcAbs, p)
			if err := copyFile(p, filepath.Join(dstAbs, rel)); err != nil {
				return 0, err
			}
		}
	}
	for _, name := range []string{"memories_1.sqlite", "session_index.jsonl"} {
		if _, err := os.Stat(filepath.Join(dstAbs, name)); os.IsNotExist(err) {
			if _, err := os.Stat(filepath.Join(srcAbs, name)); err == nil {
				if err := copyFile(filepath.Join(srcAbs, name), filepath.Join(dstAbs, name)); err != nil {
					return 0, err
				}
			}
		}
	}
	srcDB := filepath.Join(srcAbs, "state_5.sqlite")
	dstDB := filepath.Join(dstAbs, "state_5.sqlite")
	if _, err := os.Stat(srcDB); err != nil {
		return 0, nil
	}
	if _, err := os.Stat(dstDB); os.IsNotExist(err) {
		if err := copyFile(srcDB, dstDB); err != nil {
			return 0, err
		}
		db, err := sql.Open("sqlite", dstDB)
		if err != nil {
			return 0, err
		}
		defer db.Close()
		var n int
		if _, err := db.Exec(`UPDATE threads SET rollout_path = replace(rollout_path, ?, ?)`, srcAbs, dstAbs); err != nil {
			return 0, err
		}
		db.QueryRow(`SELECT count(*) FROM threads`).Scan(&n)
		return n, nil
	}
	s, err := sql.Open("sqlite", srcDB)
	if err != nil {
		return 0, err
	}
	defer s.Close()
	d, err := sql.Open("sqlite", dstDB)
	if err != nil {
		return 0, err
	}
	defer d.Close()
	srcCols, err := columns(s, "threads")
	if err != nil {
		return 0, err
	}
	dstCols, err := columns(d, "threads")
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, c := range dstCols {
		have[c] = true
	}
	var cols []string
	for _, c := range srcCols {
		if have[c] {
			cols = append(cols, c)
		}
	}
	rp := -1
	for i, c := range cols {
		if c == "rollout_path" {
			rp = i
		}
	}
	rows, err := s.Query(`SELECT ` + strings.Join(cols, ", ") + ` FROM threads`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO threads (` + strings.Join(cols, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + `)`)
	if err != nil {
		return 0, err
	}
	added := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, err
		}
		if rp >= 0 {
			if p, ok := vals[rp].(string); ok {
				vals[rp] = strings.Replace(p, srcAbs, dstAbs, 1)
			}
		}
		res, err := stmt.Exec(vals...)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("threads insert: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

func columns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, _ := in.Stat()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
