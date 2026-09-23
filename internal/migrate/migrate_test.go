package migrate

import (
	"archive/tar"
	"bytes"
	"database/sql"
	"os"
	"os/exec"
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
	if !strings.HasPrefix(rp, codexDir) || cwd != filepath.Join(newHome, "AI", "Project", "Personal", "eco") {
		t.Fatalf("threads row: %s | %s", rp, cwd)
	}
	if _, err := os.Stat(rp); err != nil {
		t.Fatalf("rollout_path does not resolve: %s", rp)
	}
}

func TestRewritePathsSeparatesStudioProjects(t *testing.T) {
	dir := t.TempDir()
	codexDir := serverHome(t, dir, []string{"id", "rollout_path", "cwd", "title", "updated_at"})
	newHome := filepath.Join(dir, "Users", "l")
	origin := "https://github.com/MOX-Studio/eco.git"
	if _, err := RewritePaths(codexDir, oldHome, newHome, Repo{Name: "eco", Origin: &origin}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow(`SELECT cwd FROM threads`).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	if cwd != filepath.Join(newHome, "AI", "Project", "MOX", "eco") {
		t.Fatalf("studio cwd: %s", cwd)
	}
}

func TestRewritePathsUsesLongestRepositoryNameFirst(t *testing.T) {
	dir := t.TempDir()
	codexDir := serverHome(t, dir, []string{"id", "rollout_path", "cwd", "title", "updated_at"})
	studio := "https://github.com/MOX-Studio/eco-old.git"
	personal := "https://github.com/alice/eco.git"
	db, err := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET cwd = ?`, oldHome+"/Claud/projects/eco-old"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	newHome := filepath.Join(dir, "Users", "l")
	if _, err := RewritePaths(codexDir, oldHome, newHome, Repo{Name: "eco", Origin: &personal}, Repo{Name: "eco-old", Origin: &studio}); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow(`SELECT cwd FROM threads`).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	if cwd != filepath.Join(newHome, "AI", "Project", "MOX", "eco-old") {
		t.Fatalf("long name rewritten incorrectly: %s", cwd)
	}
}

func TestRewriteTextPathsHandlesWindowsEscapes(t *testing.T) {
	pair := [][2]string{{`C:\Users\Lilya\MOX\projects\site`, `C:\Users\Lilya\AI\Project\MOX\site`}}
	for _, input := range []string{
		`{"cwd":"C:\\Users\\Lilya\\MOX\\projects\\site"}`,
		`cwd=C:\Users\Lilya\MOX\projects\site`,
		`cwd=C:/Users/Lilya/MOX/projects/site`,
	} {
		out := rewriteTextPaths([]byte(input), pair)
		if bytes.Contains(out, []byte("MOX/projects/site")) || bytes.Contains(out, []byte(`MOX\projects\site`)) {
			t.Fatalf("old path remains: %s", out)
		}
		if !bytes.Contains(out, []byte("AI")) {
			t.Fatalf("new root missing: %s", out)
		}
	}
}

func TestRewriteLocalPathsKeepsSiblingWithSharedPrefix(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(home, "MOX", "projects", "site")
	sibling := old + "2"
	newPath := filepath.Join(home, "AI", "Project", "MOX", "site")
	config := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(config, []byte("a="+old+"\nb="+sibling+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE threads(cwd TEXT, rollout_path TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO threads VALUES (?, ?)", sibling, sibling+"/rollout.jsonl"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := RewriteLocalPaths(codexHome, [][2]string{{old, newPath}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config)
	if err != nil || !bytes.Contains(data, []byte("a="+newPath)) || !bytes.Contains(data, []byte("b="+sibling)) {
		t.Fatalf("sibling text changed: %s %v", data, err)
	}
	db, err = sql.Open("sqlite", filepath.Join(codexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow("SELECT cwd FROM threads").Scan(&cwd); err != nil || cwd != sibling {
		t.Fatalf("sibling thread changed: %s %v", cwd, err)
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

func TestCloneReposRejectsHostileEntries(t *testing.T) {
	dir := t.TempDir()
	evil := "--upload-pack=touch /tmp/pwned"
	n, err := CloneRepos(dir, filepath.Join(dir, "projects"), []Repo{{Name: "../escape", Origin: nil}, {Name: "ok", Origin: &evil, Pushed: true}}, "", func(string) {})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(err) {
		t.Fatal("path traversal")
	}
}

func TestCloneReposRefusesUnclassifiedDraft(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "repos-no-remote", "draft")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("draft"), 0o644); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(dir, "AI", "Project")
	n, err := CloneRepos(dir, projects, []Repo{{Name: "draft"}}, "", func(string) {})
	if err == nil || n != 0 {
		t.Fatalf("unclassified draft moved: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(src, "note.txt")); err != nil {
		t.Fatal("original draft lost", err)
	}
	studio := "https://github.com/MOX-Studio/example.git"
	if repoCategory(Repo{Name: "example", Origin: &studio}) != "MOX" {
		t.Fatal("studio origin classified as personal")
	}
}

func TestClassifyDocumentedPersonalSandbox(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "repos-no-remote", "vps-development")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# vps-development\n\nЛичная песочница (git локальный, без remote).\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repos, err := ClassifyLocalOnlyRepos(dir, []Repo{{Name: "vps-development"}})
	if err != nil || len(repos) != 1 || repos[0].Category != "Personal" {
		t.Fatalf("documented personal sandbox: %+v %v", repos, err)
	}
	n, err := CloneRepos(dir, filepath.Join(dir, "AI", "Project"), repos, "", func(string) {})
	if err != nil || n != 1 {
		t.Fatalf("clone documented sandbox: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AI", "Project", "Personal", "vps-development", "README.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRehomeLocalProjectsMovesAndRewritesThreads(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "MOX", "projects")
	studio := filepath.Join(legacy, "site")
	personal := filepath.Join(legacy, "notes")
	for _, dir := range []string{studio, personal} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %s: %v", out, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("local edit"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", studio, "remote", "add", "origin", "https://github.com/MOX-Studio/site.git").CombinedOutput(); err != nil {
		t.Fatalf("origin: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", personal, "remote", "add", "origin", "https://github.com/katya/notes.git").CombinedOutput(); err != nil {
		t.Fatalf("origin: %s: %v", out, err)
	}
	codexHome := filepath.Join(home, ".codex")
	roll := filepath.Join(codexHome, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(roll, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roll, "x.jsonl"), []byte(`{"cwd":"`+studio+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(`project="`+personal+`"`), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE threads(cwd TEXT, rollout_path TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES (?, ?)`, studio, filepath.Join(roll, "x.jsonl")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	moves, err := PlanLocalProjects(home)
	if err != nil || len(moves) != 2 {
		t.Fatalf("plan: %+v %v", moves, err)
	}
	n, err := RehomeLocalProjects(home, codexHome, func(string) {})
	if err != nil || n != 2 {
		t.Fatalf("rehome: n=%d err=%v", n, err)
	}
	studioNew := filepath.Join(home, "AI", "Project", "MOX", "site")
	personalNew := filepath.Join(home, "AI", "Project", "Personal", "notes")
	for _, dir := range []string{studioNew, personalNew} {
		if data, err := os.ReadFile(filepath.Join(dir, "keep.txt")); err != nil || string(data) != "local edit" {
			t.Fatalf("project data lost: %s %v", dir, err)
		}
	}
	db, err = sql.Open("sqlite", filepath.Join(codexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow(`SELECT cwd FROM threads`).Scan(&cwd); err != nil || cwd != studioNew {
		t.Fatalf("thread cwd: %s %v", cwd, err)
	}
	if data, _ := os.ReadFile(filepath.Join(roll, "x.jsonl")); !bytes.Contains(data, []byte(studioNew)) {
		t.Fatal("rollout path was not rewritten")
	}
	if data, _ := os.ReadFile(filepath.Join(codexHome, "config.toml")); !bytes.Contains(data, []byte(personalNew)) {
		t.Fatal("config path was not rewritten")
	}
	if _, err := os.Stat(localJournal(home)); !os.IsNotExist(err) {
		t.Fatal("completed migration journal remains")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("old projects directory remains")
	}
}

func TestPlanLocalProjectsRefusesDestinationConflict(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "MOX", "projects", "same")
	dest := filepath.Join(home, "AI", "Project", "Personal", "same")
	for _, dir := range []string{legacy, dest} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PlanLocalProjects(home); err == nil {
		t.Fatal("conflicting destination accepted")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("source changed during read-only plan")
	}
}

func TestPlanLocalProjectsRefusesUnknownOwner(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "MOX", "projects", "draft")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", legacy).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	if _, err := PlanLocalProjects(home); err == nil {
		t.Fatal("project without origin classified silently")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("legacy project changed during preflight", err)
	}
}

func TestPlanLocalProjectsAcceptsDocumentedPersonalSandbox(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "MOX", "projects", "vps-development")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", legacy).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "README.md"), []byte("Личная песочница (git локальный, без remote)."), 0o644); err != nil {
		t.Fatal(err)
	}
	moves, err := PlanLocalProjects(home)
	if err != nil || len(moves) != 1 || moves[0].Category != "Personal" {
		t.Fatalf("documented sandbox plan: %+v %v", moves, err)
	}
}

func TestPlanLocalProjectsMovesTransferredPersonalRepo(t *testing.T) {
	home := t.TempDir()
	from := filepath.Join(home, "AI", "Project", "Personal", "site")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", from).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", from, "remote", "add", "origin", "git@github.com:MOX-Studio/site.git").CombinedOutput(); err != nil {
		t.Fatalf("origin: %s: %v", out, err)
	}
	moves, err := PlanLocalProjects(home)
	if err != nil || len(moves) != 1 || moves[0].Category != "MOX" {
		t.Fatalf("transfer plan: %+v %v", moves, err)
	}
	if _, err := RehomeLocalProjects(home, filepath.Join(home, ".codex"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "AI", "Project", "MOX", "site", ".git")); err != nil {
		t.Fatal(err)
	}
}
