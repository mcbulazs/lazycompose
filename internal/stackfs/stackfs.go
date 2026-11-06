// Package stackfs creates and moves environment folders, and checks that a
// target folder is one lazycompose will find again.
package stackfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mcbulazs/lazycompose/internal/discover"
)

// ComposeName is the default file name for new environments.
const ComposeName = "docker-compose.yml"

// Starter is the compose file of a new environment when no template is set.
const Starter = `services:
  app:
    image: nginx:alpine # replace with your image
    restart: unless-stopped
`

// Target is a checked folder for a new or moved environment.
type Target struct {
	Root string // scan root it is under
	Dir  string // absolute, cleaned folder
	Rel  string // Dir relative to Root, with forward slashes
}

// Env is the environment the target becomes once it has a compose file.
func (t Target) Env(file string) discover.Env {
	return discover.Env{Root: t.Root, Rel: t.Rel, Dir: t.Dir, File: filepath.Join(t.Dir, file)}
}

// Check validates path as the folder of a new or moved environment: it must
// be inside one of roots (also after following symlinks), within maxDepth,
// in a folder the scan does not skip, not inside another environment, and
// must not exist yet. self is the folder being moved, or "".
func Check(path string, roots []string, maxDepth int, envs []discover.Env, self string) (Target, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Target{}, errors.New("type a folder")
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		return Target{}, errors.New("use a full path, or one starting with ~/")
	}
	dir := filepath.Clean(path)

	var t Target
	for _, r := range roots {
		root, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if rel, ok := under(root, dir); ok {
			if t.Root == "" || len(root) > len(t.Root) {
				t = Target{Root: root, Dir: dir, Rel: rel}
			}
		}
	}
	if t.Root == "" {
		return Target{}, fmt.Errorf("%s is not inside the scanned folders (%s)", dir, strings.Join(roots, ", "))
	}
	if t.Rel == "." {
		return Target{}, errors.New("type a folder name after the path")
	}

	parts := strings.Split(t.Rel, "/")
	if maxDepth > 0 && len(parts) > maxDepth {
		return Target{}, fmt.Errorf("too deep: max_depth is %d, so the scan would not find it", maxDepth)
	}
	p := t.Root
	for _, part := range parts {
		p = filepath.Join(p, part)
		if discover.Skips(p) {
			return Target{}, fmt.Errorf("the scan skips %q folders, so it would not be found", part)
		}
	}

	if self != "" {
		if dir == self {
			return Target{}, errors.New("that is where it already is")
		}
		if _, ok := under(self, dir); ok {
			return Target{}, errors.New("cannot move a folder into itself")
		}
	}
	for _, e := range envs {
		if e.Dir == self {
			continue
		}
		if rel, ok := under(e.Dir, dir); ok && rel != "." {
			return Target{}, fmt.Errorf("inside environment %s, which the scan does not look into", e.Rel)
		}
	}

	if _, err := os.Lstat(dir); err == nil {
		return Target{}, fmt.Errorf("%s already exists", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Target{}, err
	}

	// a symlinked folder on the way could lead out of the root
	real, err := realExisting(filepath.Dir(dir))
	if err != nil {
		return Target{}, err
	}
	realRoot, err := filepath.EvalSymlinks(t.Root)
	if err != nil {
		return Target{}, err
	}
	if _, ok := under(realRoot, real); !ok {
		return Target{}, fmt.Errorf("%s leads outside %s through a symlink", filepath.Dir(dir), t.Root)
	}
	return t, nil
}

// under reports whether path is root or inside it, and the relative path.
func under(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// realExisting resolves symlinks in the deepest existing part of path and
// adds the missing rest back.
func realExisting(path string) (string, error) {
	missing := ""
	for {
		real, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(real, missing), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = filepath.Join(filepath.Base(path), missing)
		path = parent
	}
}

// Create makes the folder of t with a compose file called name holding
// compose, and an empty .env if dotenv is set. It returns the compose file
// path. name must be one of discover.ComposeFiles, or the scan would not
// find the environment.
func Create(t Target, name string, compose []byte, dotenv bool) (string, error) {
	if !slices.Contains(discover.ComposeFiles, name) {
		return "", fmt.Errorf("%q is not a compose file name the scan finds", name)
	}
	if err := os.MkdirAll(filepath.Dir(t.Dir), 0o755); err != nil {
		return "", err
	}
	// Mkdir, not MkdirAll: fails if the folder appeared since the check
	if err := os.Mkdir(t.Dir, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(t.Dir, name)
	if err := writeNew(file, compose, 0o644); err != nil {
		return "", err
	}
	if dotenv {
		if err := writeNew(filepath.Join(t.Dir, ".env"), nil, 0o600); err != nil {
			return "", err
		}
	}
	return file, nil
}

func writeNew(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Move moves the folder from to the checked target, creating missing parent
// folders. Within one filesystem it is a rename. Across filesystems it copies
// and then removes the original, and removes the partial copy if that fails.
func Move(from string, t Target) error {
	if err := os.MkdirAll(filepath.Dir(t.Dir), 0o755); err != nil {
		return err
	}
	err := os.Rename(from, t.Dir)
	if err == nil || !crossDevice(err) {
		return err
	}
	// create the target first, so only a folder made here is ever removed
	if err := os.Mkdir(t.Dir, 0o700); err != nil {
		return err
	}
	if err := copyTree(from, t.Dir); err != nil {
		_ = os.RemoveAll(t.Dir)
		return fmt.Errorf("copying to another filesystem: %w", err)
	}
	return os.RemoveAll(from)
}

// copyTree copies the contents of from into the existing folder to.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case rel == ".":
			return os.Chmod(to, info.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, dst)
		case d.IsDir():
			return os.Mkdir(dst, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copyFile(path, dst, info.Mode().Perm())
		default:
			return fmt.Errorf("%s: cannot copy special files", rel)
		}
	})
}

func copyFile(from, to string, perm os.FileMode) error {
	src, err := os.OpenFile(from, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

var (
	nameLine    = regexp.MustCompile(`(?m)^name\s*:`)
	outsidePath = regexp.MustCompile(`(?:^|[\s"':=\-])(\.\./[^\s"',]*)`)
)

// Warnings lists what changes when the environment in from moves to t:
// the project name if it comes from the folder name, and paths in the
// compose file that point outside the folder.
func Warnings(composeFile, from string, t Target) []string {
	var out []string
	b, _ := os.ReadFile(composeFile)
	if filepath.Base(from) != filepath.Base(t.Dir) && !nameLine.Match(b) {
		out = append(out, fmt.Sprintf(
			"the project name changes from %q to %q, so named volumes and networks do not carry over (set name: in the compose file to keep them)",
			projectName(filepath.Base(from)), projectName(filepath.Base(t.Dir))))
	}
	if filepath.Dir(from) != filepath.Dir(t.Dir) {
		seen := map[string]bool{}
		var paths []string
		for _, m := range outsidePath.FindAllSubmatch(b, -1) {
			if p := string(m[1]); !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		if len(paths) > 0 {
			out = append(out, "these paths point outside the folder and will point somewhere else: "+strings.Join(paths, ", "))
		}
	}
	return out
}

var notProjectChar = regexp.MustCompile(`[^a-z0-9_-]`)

// projectName is the compose project name docker derives from a folder.
func projectName(folder string) string {
	return notProjectChar.ReplaceAllString(strings.ToLower(folder), "")
}
