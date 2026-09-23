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
// those without one come whole from repos-no-remote/. Existing directories are left alone.
func CloneRepos(exportDir, projectsDir string, repos []Repo, gh string, log func(string)) (int, error) {
	if gh == "" {
		gh = "gh"
	}
	for _, category := range []string{"MOX", "Personal"} {
		if err := os.MkdirAll(filepath.Join(projectsDir, category), 0o755); err != nil {
			return 0, err
		}
	}
	done := 0
	for _, r := range repos {
		if !repoNameRe.MatchString(r.Name) || r.Name == ".." || r.Name == "." {
			log("пропуск: недопустимое имя репо в экспорте")
			continue
		}
		if r.Origin != nil && !repoOriginRe.MatchString(*r.Origin) {
			log("пропуск " + r.Name + ": origin не похож на адрес GitHub")
			continue
		}
		dest := filepath.Join(projectsDir, repoCategory(r), r.Name)
		if _, err := os.Stat(dest); err == nil {
			log("пропуск " + r.Name + ": каталог уже есть")
			continue
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
				log("пропуск " + r.Name + ": нет ни origin, ни копии в экспорте")
				continue
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
		parts := strings.Split(strings.TrimPrefix(*r.Origin, "https://github.com/"), "/")
		if len(parts) == 2 && strings.EqualFold(parts[0], "MOX-Studio") {
			return "MOX"
		}
	}
	return "Personal"
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
