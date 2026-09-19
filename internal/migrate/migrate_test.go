package migrate

import (
	"archive/tar"
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

const oldHome = "/home/lilya"

// serverHome builds the .codex of a server user as ops/mox-export-user.sh ships it (the codex/ directory of the archive).
func serverHome(t *testing.T, dir string, cols []string) string {
	t.Helper()
	codexDir := filepath.Join(dir, "codex")
	roll := filepath.Join(codexDir, "sessions", "2026", "09", "02")
	os.MkdirAll(roll, 0o755)
	rolloutPath := filepath.Join(roll, "rollout-2026-09-02T13-21-57-01a061a3-c3e9-7d03-82e8-e1a506364769.jsonl")
	os.WriteFile(rolloutPath, []byte(`{"type":"session_meta","payload":{"cwd":"`+oldHome+`/Claud/projects/eco","runtime_workspace_roots":["`+oldHome+`/Claud/projects/eco"]}}
{"type":"turn_context","payload":{"cwd":"`+oldHome+`/Claud/projects/eco","workspace_roots":["`+oldHome+`/Claud/projects/eco"]}}
{"type":"response_item","payload":{"type":"message","content":[{"type":"input_text","text":"файл `+oldHome+`/Claud/projects/eco/index.php"}]}}
`), 0o644)
	os.WriteFile(rolloutPath+".bak-before-fix", []byte(oldHome), 0o644)
	os.MkdirAll(filepath.Join(codexDir, "memories"), 0o755)
	os.WriteFile(filepath.Join(codexDir, "memories", "raw_memories.md"), []byte("cwd "+oldHome+"/Claud/projects/eco\n"), 0o644)
	os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[projects.\""+oldHome+"/Claud/projects/eco\"]\ntrust_level = \"trusted\"\n"), 0o644)
	os.WriteFile(filepath.Join(codexDir, "session_index.jsonl"), []byte(`{"id":"01a061a3-c3e9-7d03-82e8-e1a506364769","thread_name":"давай начнем"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(codexDir, "thread_history_1.sqlite"), []byte("stale offsets"), 0o644)
	db, err := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`CREATE TABLE _sqlx_migrations(version INTEGER PRIMARY KEY, description TEXT)`)
	db.Exec(`INSERT INTO _sqlx_migrations VALUES (54, 'x')`)
	db.Exec(`CREATE TABLE threads(` + strings.Join(cols, " TEXT, ") + ` TEXT)`)
	db.Exec(`INSERT INTO threads(id, rollout_path, cwd, title, updated_at) VALUES ('01a061a3-c9', ?, ?, 'давай начнем', 1789000000)`, oldHome+"/.codex/sessions/2026/09/02/rollout-2026-09-02T13-21-57-01a061a3-c3e9-7d03-82e8-e1a506364769.jsonl", oldHome+"/Claud/projects/eco")
	return codexDir
}

func TestRewritePaths(t *testing.T) {
	dir := t.TempDir()
	codexDir := serverHome(t, dir, []string{"id", "rollout_path", "cwd", "title", "updated_at"})
	newHome := filepath.Join(dir, "Users", "l")
	st, err := RewritePaths(codexDir, oldHome, newHome)
	if err != nil {
		t.Fatal(err)
	}
	if st.FilesChanged != 3 || st.ThreadsLeft != 0 {
		t.Fatalf("stats: %+v", st)
	}
	var left int
	filepath.WalkDir(codexDir, func(p string, d os.DirEntry, _ error) error {
		if d.IsDir() || strings.HasSuffix(p, ".sqlite") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte(oldHome)) && !strings.Contains(p, ".bak-") {
			left++
			t.Errorf("still has old home: %s", p)
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(codexDir, "thread_history_1.sqlite")); !os.IsNotExist(err) {
		t.Fatal("thread_history must be dropped")
	}
	db, _ := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	defer db.Close()
	var rp, cwd string
	db.QueryRow(`SELECT rollout_path, cwd FROM threads`).Scan(&rp, &cwd)
	if !strings.HasPrefix(rp, codexDir) || cwd != filepath.Join(newHome, "Claud", "projects", "eco") {
		t.Fatalf("threads row: %s | %s", rp, cwd)
	}
	if _, err := os.Stat(rp); err != nil {
		t.Fatalf("rollout_path does not resolve: %s", rp)
	}
}

func TestMergeThreadsAcrossSchemaVersions(t *testing.T) {
	dir := t.TempDir()
	src := serverHome(t, filepath.Join(dir, "src"), []string{"id", "rollout_path", "cwd", "title", "updated_at"})
	newHome := filepath.Join(dir, "Users", "l")
	if _, err := RewritePaths(src, oldHome, newHome); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(newHome, ".codex")
	os.MkdirAll(dst, 0o755)
	db, _ := sql.Open("sqlite", filepath.Join(dst, "state_5.sqlite"))
	db.Exec(`CREATE TABLE _sqlx_migrations(version INTEGER PRIMARY KEY, description TEXT)`)
	db.Exec(`INSERT INTO _sqlx_migrations VALUES (55, 'y')`)
	db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY, rollout_path TEXT, cwd TEXT, title TEXT, updated_at TEXT, daybreak_enabled INTEGER)`) // one column more than the server
	db.Exec(`INSERT INTO threads(id, rollout_path, cwd, title) VALUES ('local-1', '/x', '/y', 'mine')`)
	db.Close()
	n, err := MergeThreads(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("merged %d", n)
	}
	db, _ = sql.Open("sqlite", filepath.Join(dst, "state_5.sqlite"))
	defer db.Close()
	var count int
	db.QueryRow(`SELECT count(*) FROM threads`).Scan(&count)
	if count != 2 {
		t.Fatalf("threads after merge: %d", count)
	}
	var rp string
	db.QueryRow(`SELECT rollout_path FROM threads WHERE id='01a061a3-c9'`).Scan(&rp)
	if !strings.HasPrefix(rp, dst) {
		t.Fatalf("rollout_path not moved to the local home: %s", rp)
	}
	if _, err := os.Stat(rp); err != nil {
		t.Fatalf("merged rollout missing: %s", rp)
	}
	if n, _ := MergeThreads(src, dst); n != 0 {
		t.Fatalf("second merge inserted %d", n)
	}
}

func TestMergeIntoEmptyHomeCopiesState(t *testing.T) {
	dir := t.TempDir()
	src := serverHome(t, filepath.Join(dir, "src"), []string{"id", "rollout_path", "cwd", "title", "updated_at"})
	newHome := filepath.Join(dir, "Users", "l")
	RewritePaths(src, oldHome, newHome)
	dst := filepath.Join(newHome, ".codex")
	if n, err := MergeThreads(src, dst); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "state_5.sqlite")); err != nil {
		t.Fatal("state_5 must be copied when the local home has none")
	}
}

func TestExtractRefusesEscapes(t *testing.T) {
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	tw.WriteHeader(&tar.Header{Name: "codex/ok.txt", Mode: 0o644, Size: 2})
	tw.Write([]byte("ok"))
	tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1})
	tw.Write([]byte("x"))
	tw.Close()
	zw.Close()
	dir := t.TempDir()
	arc := filepath.Join(dir, "a.tar.zst")
	os.WriteFile(arc, buf.Bytes(), 0o600)
	out := filepath.Join(dir, "out")
	if err := Extract(arc, out); err == nil {
		t.Fatal("escape must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); !os.IsNotExist(err) {
		t.Fatal("escaped file written")
	}
}
