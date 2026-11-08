package stackfs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

// The remote versions of Check, Create and Move work on a folder of a
// remote root, over ssh. Paths there always use slashes.

// CheckRemote is Check for target, a folder like ssh://host/srv/web. roots
// are all the roots; only those on the same host count. self is the folder
// being moved, on that host, or "".
func CheckRemote(ctx context.Context, target string, roots []string, maxDepth int, envs []discover.Env, self string) (Target, error) {
	h, dir, err := remote.Parse(strings.TrimSpace(target))
	if err != nil {
		return Target{}, err
	}
	var t Target
	var shown []string
	rootDir := ""
	for _, r := range roots {
		rh, rdir, err := remote.Parse(r)
		if err != nil || rh.String() != h.String() {
			continue
		}
		shown = append(shown, r)
		// the deepest root it is in, as Check does
		if rel, ok := underSlash(rdir, dir); ok && (t.Root == "" || len(rdir) > len(rootDir)) {
			t = Target{Root: rh.Key(rdir), Dir: dir, Rel: rel, Host: rh}
			rootDir = rdir
		}
	}
	if t.Root == "" {
		if len(shown) == 0 {
			return Target{}, fmt.Errorf("%s is not a scanned host", h.Label())
		}
		return Target{}, fmt.Errorf("%s is not inside the scanned folders (%s)", dir, strings.Join(shown, ", "))
	}
	if t.Rel == "." {
		return Target{}, errors.New("type a folder name after the path")
	}

	parts := strings.Split(t.Rel, "/")
	if maxDepth > 0 && len(parts) > maxDepth {
		return Target{}, fmt.Errorf("too deep: max_depth is %d, so the scan would not find it", maxDepth)
	}
	p := rootDir
	for _, part := range parts {
		p = path.Join(p, part)
		if discover.Skips(p) {
			return Target{}, fmt.Errorf("the scan skips %q folders, so it would not be found", part)
		}
	}

	if self != "" {
		if dir == self {
			return Target{}, errors.New("that is where it already is")
		}
		if _, ok := underSlash(self, dir); ok {
			return Target{}, errors.New("cannot move a folder into itself")
		}
	}
	for _, e := range envs {
		if e.Host == nil || e.Host.String() != h.String() || e.Path == self {
			continue
		}
		if rel, ok := underSlash(e.Path, dir); ok && rel != "." {
			return Target{}, fmt.Errorf("inside environment %s, which the scan does not look into", e.Rel)
		}
	}

	if there, err := h.Exists(ctx, dir); err != nil {
		return Target{}, err
	} else if there {
		return Target{}, fmt.Errorf("%s already exists on %s", dir, h.Label())
	}

	// a symlinked folder on the way could lead out of the root
	real, err := realExistingRemote(ctx, h, path.Dir(dir))
	if err != nil {
		return Target{}, err
	}
	realRoot, err := h.RealPath(ctx, rootDir)
	if err != nil {
		return Target{}, err
	}
	if _, ok := underSlash(realRoot, real); !ok {
		return Target{}, fmt.Errorf("%s leads outside %s through a symlink", path.Dir(dir), rootDir)
	}
	return t, nil
}

// underSlash is under for slash paths.
func underSlash(root, p string) (string, bool) {
	root, p = path.Clean(root), path.Clean(p)
	switch {
	case p == root:
		return ".", true
	case root == "/":
		return p[1:], true
	case strings.HasPrefix(p, root+"/"):
		return p[len(root)+1:], true
	}
	return "", false
}

// realExistingRemote resolves the links of the deepest part of p that is
// there, and adds the missing rest back.
func realExistingRemote(ctx context.Context, h *remote.Host, p string) (string, error) {
	missing := ""
	for {
		there, err := h.Exists(ctx, p)
		if err != nil {
			return "", err
		}
		if there {
			real, err := h.RealPath(ctx, p)
			if err != nil {
				return "", err
			}
			return path.Join(real, missing), nil
		}
		if p == "/" {
			return "", fmt.Errorf("%s: no folder of the path is there", h.Label())
		}
		missing = path.Join(path.Base(p), missing)
		p = path.Dir(p)
	}
}

// CreateRemote is Create for a remote target. The compose file can be read
// by others, like one made here; the .env file only by its owner.
func CreateRemote(ctx context.Context, t Target, name string, compose []byte, dotenv bool) (string, error) {
	if !slices.Contains(discover.ComposeFiles, name) {
		return "", fmt.Errorf("%q is not a compose file name the scan finds", name)
	}
	h := t.Host
	if err := h.Run(ctx, "mkdir", "-p", "--", path.Dir(t.Dir)); err != nil {
		return "", err
	}
	// mkdir without -p: fails if the folder appeared since the check
	if err := h.Run(ctx, "mkdir", "--", t.Dir); err != nil {
		return "", err
	}
	file := path.Join(t.Dir, name)
	if err := h.WriteFile(ctx, file, compose, "644"); err != nil {
		return "", err
	}
	if dotenv {
		if err := h.WriteFile(ctx, path.Join(t.Dir, ".env"), nil, "600"); err != nil {
			return "", err
		}
	}
	return file, nil
}

// MoveRemote moves the folder from on the host to the checked target,
// creating missing parent folders. mv renames it, or copies it to another
// filesystem and then removes the original.
func MoveRemote(ctx context.Context, from string, t Target) error {
	h := t.Host
	if err := h.Run(ctx, "mkdir", "-p", "--", path.Dir(t.Dir)); err != nil {
		return err
	}
	// mv moves into a folder that is there, so check again right before
	if there, err := h.Exists(ctx, t.Dir); err != nil {
		return err
	} else if there {
		return fmt.Errorf("%s appeared on %s meanwhile", t.Dir, h.Label())
	}
	return h.Run(ctx, "mv", "--", from, t.Dir)
}
