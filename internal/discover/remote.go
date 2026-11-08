package discover

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mcbulazs/lazycompose/internal/remote"
)

// remoteScanTimeout bounds the scan of one remote root.
const remoteScanTimeout = time.Minute

// FindRoots scans the roots: folders here with Find, and remote roots like
// ssh://host/srv over ssh, all at the same time. A remote root that fails
// does not stop the others; its error comes back with what was found.
// An error in a local root fails the scan, as with Find.
func FindRoots(roots []string, maxDepth int) ([]Env, error) {
	var local, remotes []string
	for _, r := range roots {
		if remote.IsRemote(r) {
			remotes = append(remotes, r)
		} else {
			local = append(local, r)
		}
	}
	found := make([][]Env, len(remotes))
	errs := make([]error, len(remotes))
	var wg sync.WaitGroup
	for i, r := range remotes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), remoteScanTimeout)
			defer cancel()
			found[i], errs[i] = FindRemote(ctx, r, maxDepth)
		}()
	}
	var envs []Env
	if len(local) > 0 {
		var err error
		if envs, err = Find(local, maxDepth); err != nil {
			wg.Wait()
			return nil, err
		}
	}
	wg.Wait()
	for _, f := range found {
		envs = append(envs, f...)
	}
	sortEnvs(envs)
	return envs, errors.Join(errs...)
}

// FindRemote scans a remote root with one find command on the host. find
// leaves out hidden folders and the like; the rest of the rules are the
// ones Find uses, applied here to the files it lists.
func FindRemote(ctx context.Context, root string, maxDepth int) ([]Env, error) {
	h, dir, err := remote.Parse(root)
	if err != nil {
		return nil, err
	}
	start := dir
	if start != "/" {
		start += "/" // follow the root if it is a link, like Find does
	}
	args := []string{"find", start, "-mindepth", "1"}
	if maxDepth > 0 {
		args = append(args, "-maxdepth", strconv.Itoa(maxDepth+1))
	}
	args = append(args, "(", "-type", "d", "(", "-name", ".*")
	for _, name := range sortedSkipDirs() {
		args = append(args, "-o", "-name", name)
	}
	args = append(args, ")", "-prune", ")", "-o", "(", "-type", "f", "-o", "-type", "l", ")", "(")
	for i, name := range ComposeFiles {
		if i > 0 {
			args = append(args, "-o")
		}
		args = append(args, "-name", name)
	}
	args = append(args, ")", "-print0")

	cmd := h.Command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// find exits with 1 when it could not read some folders, and still
		// lists the rest; ssh itself exits with 255
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 || len(out) == 0 {
			if ee != nil && ee.ExitCode() == 255 && stderr.Len() > 0 {
				return nil, remote.Explain(h, errors.New(stderr.String()))
			}
			if msg := firstLine(stderr.String()); msg != "" {
				err = errors.New(msg)
			}
			return nil, remote.Explain(h, err)
		}
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	var envs []Env
	for _, f := range FromFiles(dir, files, maxDepth) {
		rel := strings.TrimPrefix(strings.TrimPrefix(f.Dir, dir), "/")
		envs = append(envs, Env{
			Root: h.Key(dir),
			Rel:  rel,
			Dir:  h.Key(f.Dir),
			File: f.File,
			Host: h,
			Path: f.Dir,
		})
	}
	return envs, nil
}

func sortedSkipDirs() []string {
	names := make([]string, 0, len(skipDirs))
	for n := range skipDirs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Found is an environment folder and its compose file, as slash paths.
type Found struct{ Dir, File string }

// FromFiles applies Find's rules to a list of compose files under root, all
// slash paths: folders Skips leaves out, the depth limit, the first compose
// file in ComposeFiles order, and no environments inside environments. A
// compose file in root itself does not count.
func FromFiles(root string, files []string, maxDepth int) []Found {
	root = path.Clean(root)
	rank := map[string]int{}
	for i, n := range ComposeFiles {
		rank[n] = i
	}
	best := map[string]string{}
	for _, f := range files {
		if strings.ContainsFunc(f, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			continue
		}
		f = path.Clean(f)
		dir, name := path.Split(f)
		dir = path.Clean(dir)
		if _, ok := rank[name]; !ok || dir == root {
			continue
		}
		rel, ok := strings.CutPrefix(dir, strings.TrimSuffix(root, "/")+"/")
		if !ok {
			continue
		}
		parts := strings.Split(rel, "/")
		if maxDepth > 0 && len(parts) > maxDepth {
			continue
		}
		skip := false
		for i := range parts {
			if Skips(path.Join(root, path.Join(parts[:i+1]...))) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if cur, ok := best[dir]; !ok || rank[name] < rank[path.Base(cur)] {
			best[dir] = f
		}
	}
	dirs := make([]string, 0, len(best))
	for d := range best {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	var out []Found
	for _, d := range dirs {
		inside := false
		for p := path.Dir(d); p != root && p != "/" && p != "."; p = path.Dir(p) {
			if _, ok := best[p]; ok {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, Found{Dir: d, File: best[d]})
		}
	}
	return out
}

// firstLine is the first line of s that is not empty, without control
// characters.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, l)
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
