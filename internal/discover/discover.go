package discover

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mcbulazs/lazycompose/internal/remote"
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
	Rel  string // path relative to Root, e.g. "web/blog"
	// Dir is the absolute folder. For a remote environment it is
	// ssh://host/path, which only names it; Path is the folder there.
	Dir  string
	File string // absolute path to the compose file, on Host
	// Host is the machine a remote environment is on; nil is this one.
	Host *remote.Host
	Path string // the folder on Host, for a remote environment
}

// Remote reports whether the environment is on another machine.
func (e Env) Remote() bool { return e.Host != nil }

// Folder is the environment's folder on the machine it is on.
func (e Env) Folder() string {
	if e.Host != nil {
		return e.Path
	}
	return e.Dir
}

// Name is the folder name of the environment.
func (e Env) Name() string {
	if e.Host != nil {
		return path.Base(e.Path)
	}
	return filepath.Base(e.Dir)
}

// RootName is a short name for the root, for headings: its folder name,
// with the host in front for a remote root.
func (e Env) RootName() string {
	if e.Host != nil {
		return e.Host.Label() + ":" + path.Base(strings.TrimPrefix(e.Root, e.Host.String()))
	}
	return filepath.Base(e.Root)
}

// Group is the parent folder relative to the root, or "" for top level.
func (e Env) Group() string {
	g := filepath.Dir(e.Rel)
	if g == "." {
		return ""
	}
	return filepath.ToSlash(g)
}

// skipDirs never hold stacks of their own, only dependencies or fixtures.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"testdata":     true,
}

// Skips reports whether scanning skips the folder at path: hidden folders,
// node_modules, vendor, testdata and the Go module cache.
func Skips(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") || skipDirs[name] {
		return true
	}
	return name == "mod" && filepath.Base(filepath.Dir(path)) == "pkg"
}

// Find scans each root up to maxDepth folders deep, or without limit when
// maxDepth is 0. It does not descend into an environment once found, so app
// data folders are not walked.
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
			if path != root && Skips(path) {
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

			if maxDepth > 0 && depth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sortEnvs(envs)
	return envs, nil
}

func sortEnvs(envs []Env) {
	sort.SliceStable(envs, func(i, j int) bool {
		if envs[i].Root != envs[j].Root {
			return envs[i].Root < envs[j].Root
		}
		return envs[i].Rel < envs[j].Rel
	})
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
