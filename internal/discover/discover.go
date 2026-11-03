package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ComposeFiles are checked in the same order docker compose uses.
var ComposeFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// Env is a folder that contains a compose file.
type Env struct {
	Root string // scan root it was found under
	Rel  string // path relative to Root, e.g. "web/blogger"
	Dir  string // absolute folder
	File string // absolute path to the compose file
}

// Name is the folder name of the environment.
func (e Env) Name() string { return filepath.Base(e.Dir) }

// Group is the parent folder relative to the root, or "" for top level.
func (e Env) Group() string {
	g := filepath.Dir(e.Rel)
	if g == "." {
		return ""
	}
	return filepath.ToSlash(g)
}

// Find scans each root up to maxDepth folders deep. It does not descend into
// an environment once found, so app data folders are not walked.
func Find(roots []string, maxDepth int) ([]Env, error) {
	var envs []Env
	seen := map[string]bool{}

	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(root); err != nil {
			return nil, err
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// unreadable folders are skipped, not fatal
				if path != root && d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}

			rel, _ := filepath.Rel(root, path)
			depth := 0
			if rel != "." {
				depth = strings.Count(rel, string(filepath.Separator)) + 1
			}

			if file := composeFile(path); file != "" && rel != "." {
				if !seen[path] {
					seen[path] = true
					envs = append(envs, Env{
						Root: root,
						Rel:  filepath.ToSlash(rel),
						Dir:  path,
						File: file,
					})
				}
				return fs.SkipDir
			}

			if depth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.SliceStable(envs, func(i, j int) bool {
		if envs[i].Root != envs[j].Root {
			return envs[i].Root < envs[j].Root
		}
		return envs[i].Rel < envs[j].Rel
	})
	return envs, nil
}

func composeFile(dir string) string {
	for _, name := range ComposeFiles {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
