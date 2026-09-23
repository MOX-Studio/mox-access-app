package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type LocalMove struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	From     string `json:"from"`
	To       string `json:"to"`
}

var githubOwner = regexp.MustCompile(`(?i)^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+)/`)

func localJournal(home string) string { return filepath.Join(home, "AI", ".workspace-migration.json") }

// PlanLocalProjects is read-only. It refuses links and destination conflicts before any project is moved.
func PlanLocalProjects(home string) ([]LocalMove, error) {
	journal := localJournal(home)
	if info, err := os.Lstat(journal); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: нужен обычный файл журнала", journal)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if data, err := os.ReadFile(journal); err == nil {
		var moves []LocalMove
		if err := json.Unmarshal(data, &moves); err != nil {
			return nil, fmt.Errorf("журнал переноса AI повреждён: %w", err)
		}
		for _, move := range moves {
			if err := validateLocalMove(home, move); err != nil {
				return nil, err
			}
		}
		return moves, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var moves []LocalMove
	for _, source := range []struct {
		Root     string
		Transfer bool
	}{
		{filepath.Join(home, "MOX", "projects"), false},
		{filepath.Join(home, "AI", "Project", "Personal"), true},
	} {
		info, err := os.Lstat(source.Root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: ожидался обычный каталог проектов", source.Root)
		}
		entries, err := os.ReadDir(source.Root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Name() == ".DS_Store" {
				continue
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("%s: найден не каталог проекта; разберите вручную", filepath.Join(source.Root, entry.Name()))
			}
			from := filepath.Join(source.Root, entry.Name())
			category, categoryErr := categoryOfLocalRepo(from)
			if categoryErr != nil {
				if source.Transfer {
					continue
				}
				return nil, fmt.Errorf("%s: невозможно определить личный или студийный проект: %w", from, categoryErr)
			}
			if source.Transfer && category != "MOX" {
				continue
			}
			move := LocalMove{Name: entry.Name(), Category: category, From: from,
				To: filepath.Join(home, "AI", "Project", category, entry.Name())}
			if err := validateLocalMove(home, move); err != nil {
				return nil, err
			}
			if _, err := os.Lstat(move.To); err == nil {
				return nil, fmt.Errorf("%s: целевая папка уже существует; автоматический перенос остановлен", move.To)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			moves = append(moves, move)
		}
	}
	return moves, nil
}

func categoryOfLocalRepo(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		if documentedPersonal(dir) {
			return "Personal", nil
		}
		return "", fmt.Errorf("у репозитория нет читаемого origin; укажите GitHub remote перед переносом")
	}
	match := githubOwner.FindStringSubmatch(strings.TrimSpace(string(out)))
	if len(match) != 2 {
		return "", fmt.Errorf("origin не содержит определяемого владельца GitHub")
	}
	if strings.EqualFold(match[1], "MOX-Studio") {
		return "MOX", nil
	}
	return "Personal", nil
}

func documentedPersonal(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	return err == nil && strings.Contains(string(data), "Личная песочница")
}

func validateLocalMove(home string, move LocalMove) error {
	if move.Name == "" || move.Name == "." || move.Name == ".." || filepath.Base(move.Name) != move.Name ||
		move.Category != "MOX" && move.Category != "Personal" ||
		move.To != filepath.Join(home, "AI", "Project", move.Category, move.Name) {
		return fmt.Errorf("журнал переноса AI содержит недопустимый путь")
	}
	legacy := filepath.Join(home, "MOX", "projects", move.Name)
	transfer := filepath.Join(home, "AI", "Project", "Personal", move.Name)
	if move.From != legacy && (move.From != transfer || move.Category != "MOX") {
		return fmt.Errorf("журнал переноса AI содержит недопустимый источник")
	}
	return nil
}

// RehomeLocalProjects moves known legacy repositories and rewrites Codex paths while Codex is closed.
// The journal stays in ~/AI until both file moves and path rewrites complete, so a retry resumes safely.
func RehomeLocalProjects(home, codexHome string, log func(string)) (int, error) {
	moves, err := PlanLocalProjects(home)
	if err != nil || len(moves) == 0 {
		return 0, err
	}
	if log == nil {
		log = func(string) {}
	}
	journal := localJournal(home)
	for _, dir := range []string{filepath.Join(home, "AI"), filepath.Join(home, "AI", "Project"), filepath.Join(home, "AI", "Project", "MOX"), filepath.Join(home, "AI", "Project", "Personal")} {
		info, statErr := os.Lstat(dir)
		if statErr == nil && !info.IsDir() {
			return 0, fmt.Errorf("%s: ожидался обычный каталог", dir)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return 0, statErr
		}
	}
	if _, err := os.Stat(journal); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(moves, "", "  ")
		if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
			return 0, err
		}
		tmp, err := os.CreateTemp(filepath.Dir(journal), ".workspace-migration-")
		if err != nil {
			return 0, err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return 0, err
		}
		if err := tmp.Close(); err != nil {
			return 0, err
		}
		if err := os.Rename(tmp.Name(), journal); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	pairs := make([][2]string, 0, len(moves))
	for _, move := range moves {
		if err := validateLocalMove(home, move); err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(move.To), 0o755); err != nil {
			return 0, err
		}
		_, fromErr := os.Lstat(move.From)
		_, toErr := os.Lstat(move.To)
		switch {
		case fromErr == nil && os.IsNotExist(toErr):
			log("→ переношу " + move.Name + " → " + move.Category)
			if err := os.Rename(move.From, move.To); err != nil {
				return 0, fmt.Errorf("%s: %w", move.Name, err)
			}
		case os.IsNotExist(fromErr) && toErr == nil:
			log("→ уже перенесён " + move.Name)
		default:
			return 0, fmt.Errorf("%s: исходная и целевая папки в неожиданном состоянии; журнал сохранён", move.Name)
		}
		pairs = append(pairs, [2]string{move.From, move.To})
	}
	stats, err := RewriteLocalPaths(codexHome, pairs)
	if err != nil {
		return 0, fmt.Errorf("пути Codex не обновлены; журнал сохранён: %w", err)
	}
	if stats.ThreadsLeft != 0 {
		return 0, fmt.Errorf("остались треды со старым путём (%d); журнал сохранён", stats.ThreadsLeft)
	}
	if err := os.Remove(journal); err != nil {
		return 0, err
	}
	_ = os.Remove(filepath.Join(home, "MOX", "projects", ".DS_Store"))
	_ = os.Remove(filepath.Join(home, "MOX", "projects"))
	_ = os.Remove(filepath.Join(home, "MOX"))
	return len(moves), nil
}
