package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MOX-Studio/mox-access-app/internal/hide"
)

// repos.json comes from the server export; names become directories and origins become gh arguments, so both are
// checked here before anything is executed or created.
var (
	repoNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,99}$`)
	repoOriginRe = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(\.git)?$`)
)

// CloneRepos recreates ~/AI/Project/{MOX,Personal}: repositories with an origin are cloned with gh under the employee's own
// account and switched to the branch the server was on (the WIP branch when there was uncommitted work);
// unpublished repositories with an origin come whole from repos-no-remote/.
// Repositories without an origin stop before writing because their owner cannot be inferred.
func CloneRepos(exportDir, projectsDir string, repos []Repo, gh string, log func(string)) (int, error) {
	if gh == "" {
		gh = "gh"
	}
	for _, r := range repos {
		if !repoNameRe.MatchString(r.Name) || r.Name == "." || r.Name == ".." {
			return 0, fmt.Errorf("%q: недопустимое имя проекта в экспорте", r.Name)
		}
		if r.Origin != nil && !repoOriginRe.MatchString(*r.Origin) {
			return 0, fmt.Errorf("%s: неподдерживаемый GitHub origin в экспорте", r.Name)
		}
		if repoCategory(r) == "" {
			return 0, fmt.Errorf("%s: у проекта нет GitHub origin; нельзя определить MOX или Personal без решения владельца", r.Name)
		}
	}
	for _, category := range []string{"MOX", "Personal"} {
		if err := os.MkdirAll(filepath.Join(projectsDir, category), 0o755); err != nil {
			return 0, err
		}
	}
	done := 0
	for _, r := range repos {
		// Every entry was validated before any directory was created.
		dest := filepath.Join(projectsDir, repoCategory(r), r.Name)
		if info, err := os.Lstat(dest); err == nil {
			if !info.IsDir() {
				return done, fmt.Errorf("%s: целевой путь уже занят", dest)
			}
			if r.Origin != nil {
				out, gitErr := exec.Command("git", "-C", dest, "remote", "get-url", "origin").Output()
				if gitErr != nil || strings.TrimSpace(string(out)) != *r.Origin {
					return done, fmt.Errorf("%s: существующий проект имеет другой GitHub origin", dest)
				}
			} else if !documentedPersonal(dest) {
				return done, fmt.Errorf("%s: существующий проект нельзя сверить с экспортом", dest)
			}
			log("проект уже на месте: " + r.Name)
			continue
		} else if !os.IsNotExist(err) {
			return done, err
		}
		if r.Origin != nil && r.Pushed {
			log("клонирую " + r.Name)
			cmd := hide.Cmd(exec.Command(gh, "repo", "clone", "--", *r.Origin, dest))
			if out, err := cmd.CombinedOutput(); err != nil {
				return done, fmt.Errorf("клон %s: %v: %s", r.Name, err, out)
			}
			if r.Branch != "" && r.Branch != "HEAD" {
				if out, err := hide.Cmd(exec.Command("git", "-C", dest, "checkout", "-q", "--", r.Branch)).CombinedOutput(); err != nil {
					log(fmt.Sprintf("ветка %s в %s не переключилась: %s", r.Branch, r.Name, out))
				}
			}
		} else {
			src := filepath.Join(exportDir, "repos-no-remote", r.Name)
			if _, err := os.Stat(src); err != nil {
				return done, fmt.Errorf("%s: нет копии проекта в экспорте: %w", r.Name, err)
			}
			log("копирую " + r.Name + " (без origin)")
			if err := copyTree(src, dest); err != nil {
				return done, fmt.Errorf("копия %s: %w", r.Name, err)
			}
		}
		done++
	}
	return done, nil
}

func repoCategory(r Repo) string {
	if r.Origin != nil {
		if !repoOriginRe.MatchString(*r.Origin) {
			return ""
		}
		parts := strings.Split(strings.TrimPrefix(*r.Origin, "https://github.com/"), "/")
		if len(parts) == 2 {
			if strings.EqualFold(parts[0], "MOX-Studio") {
				return "MOX"
			}
			return "Personal"
		}
	}
	if r.Origin == nil && r.Category == "Personal" {
		return "Personal"
	}
	return ""
}

// ClassifyLocalOnlyRepos recognises a documented personal sandbox in the server export.
// Other unpublished projects stop before cloning so studio ownership is never guessed.
func ClassifyLocalOnlyRepos(exportDir string, repos []Repo) ([]Repo, error) {
	classified := append([]Repo(nil), repos...)
	for i := range classified {
		r := &classified[i]
		if !repoNameRe.MatchString(r.Name) || r.Name == "." || r.Name == ".." {
			return nil, fmt.Errorf("%q: недопустимое имя проекта в экспорте", r.Name)
		}
		if r.Origin != nil {
			if !repoOriginRe.MatchString(*r.Origin) {
				return nil, fmt.Errorf("%s: неподдерживаемый GitHub origin в экспорте", r.Name)
			}
			continue
		}
		if documentedPersonal(filepath.Join(exportDir, "repos-no-remote", r.Name)) {
			r.Category = "Personal"
			continue
		}
		return nil, fmt.Errorf("%s: нет GitHub origin и явного личного статуса; требуется классификация перед переносом", r.Name)
	}
	return classified, nil
}

// copyTree copies a directory with its permissions, portable (cp -a is not on Windows); symlinks are recreated.
func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dest, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	})
}
