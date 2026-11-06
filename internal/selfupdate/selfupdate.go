// Package selfupdate finds the latest lazycompose release on GitHub and
// replaces the running binary with it.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is where the releases are published.
const Repo = "mcbulazs/lazycompose"

// Limits on what is read from GitHub.
const (
	maxChecksums = 64 << 10
	maxArchive   = 64 << 20
	maxBinary    = 128 << 20
)

// hosts that release downloads may be served from
var downloadHosts = map[string]bool{
	"github.com":                            true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
	"github-releases.githubusercontent.com": true,
}

// ErrUpToDate is returned by Update when there is no newer release.
var ErrUpToDate = errors.New("already the latest version")

type Client struct {
	HTTP *http.Client
	// Base is https://github.com; tests point it at a local server.
	Base         string
	GOOS, GOARCH string
}

func New() *Client {
	c := &Client{Base: "https://github.com", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	c.HTTP = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: c.checkRedirect}
	return c
}

// checkRedirect allows redirects only to GitHub's download hosts over
// https, or to the base host itself.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if !c.allowed(req.URL) {
		return fmt.Errorf("refusing to download from %s", req.URL.Host)
	}
	return nil
}

func (c *Client) allowed(u *url.URL) bool {
	if base, err := url.Parse(c.Base); err == nil && u.Scheme == base.Scheme && u.Host == base.Host {
		return true
	}
	return u.Scheme == "https" && downloadHosts[u.Hostname()]
}

var tagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// release parses a release tag like v1.2.3. Pre-releases and development
// builds are not releases.
func release(tag string) ([3]int, bool) {
	var v [3]int
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return v, false
	}
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// IsRelease reports whether version is a release tag like v1.2.3.
func IsRelease(version string) bool {
	_, ok := release(version)
	return ok
}

// Newer reports whether latest is a newer release than current. A current
// version that is not a release is never older.
func Newer(latest, current string) bool {
	l, ok1 := release(latest)
	c, ok2 := release(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// Latest returns the tag of the latest release. It reads where GitHub's
// releases/latest page redirects to, which needs no API token and has no
// API rate limit.
func (c *Client) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.Base+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	noFollow := *c.HTTP
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := noFollow.Do(req)
	if err != nil {
		return "", err
	}
	_ = res.Body.Close()
	if res.StatusCode < 300 || res.StatusCode > 399 {
		return "", fmt.Errorf("looking up the latest release: %s", res.Status)
	}
	loc, err := res.Location()
	if err != nil {
		return "", fmt.Errorf("looking up the latest release: %w", err)
	}
	_, tag, ok := strings.Cut(loc.Path, "/releases/tag/")
	if !ok || !IsRelease(tag) {
		return "", fmt.Errorf("looking up the latest release: unexpected answer %q", loc.Path)
	}
	return tag, nil
}

// managedIn are the places package managers install into, after
// symlinks are resolved: the nix store, Homebrew's Cellar (macOS and
// Linux), Scoop and winget.
var managedIn = []string{"/nix/store/", "/Cellar/", "/scoop/apps/", "/WinGet/Packages/"}

// Managed reports whether the binary at exe belongs to a package manager
// (in /usr/bin, /bin, the nix store, Homebrew, Scoop or winget). Replacing
// it would go behind the package manager's back, so those are updated with
// it instead.
func Managed(exe string) bool {
	p := strings.ReplaceAll(exe, `\`, "/")
	switch path.Dir(p) {
	case "/usr/bin", "/bin", "/usr/sbin", "/sbin":
		return true
	}
	for _, m := range managedIn {
		if strings.Contains(strings.ToLower(p), strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// AssetName is the release archive for this system: a .tar.gz, or a .zip
// on Windows.
func (c *Client) AssetName() (string, error) {
	if osNames[c.GOOS] == "" || (c.GOARCH != "amd64" && c.GOARCH != "arm64") {
		return "", fmt.Errorf("there are release binaries for Linux, macOS and Windows on amd64 and arm64 only, not %s %s", c.GOOS, c.GOARCH)
	}
	ext := "tar.gz"
	if c.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("lazycompose_%s_%s.%s", c.GOOS, c.GOARCH, ext), nil
}

// binName is the name of the binary in the archive.
func (c *Client) binName() string {
	if c.GOOS == "windows" {
		return "lazycompose.exe"
	}
	return "lazycompose"
}

// Update replaces the binary at exe with the latest release if it is newer
// than current, and returns the new version. The archive is checked against
// the release's checksums, and the binary is swapped in with a rename, so a
// failure leaves the old binary in place.
func (c *Client) Update(ctx context.Context, current, exe string) (string, error) {
	if !IsRelease(current) {
		return "", fmt.Errorf("this is a development build (%s), not a release; install a release first, see the README", current)
	}
	if Managed(exe) {
		return "", fmt.Errorf("%s was installed by a package manager; update it with that instead", exe)
	}
	asset, err := c.AssetName()
	if err != nil {
		return "", err
	}
	tag, err := c.Latest(ctx)
	if err != nil {
		return "", err
	}
	if !Newer(tag, current) {
		return tag, ErrUpToDate
	}

	dl := c.Base + "/" + Repo + "/releases/download/" + tag + "/"
	sums, err := c.get(ctx, dl+"checksums.txt", maxChecksums)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", err
	}
	archive, err := c.get(ctx, dl+asset, maxArchive)
	if err != nil {
		return "", err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s does not match its checksum, not installing it", asset)
	}
	bin, err := extract(archive, c.binName())
	if err != nil {
		return "", err
	}
	if err := checkBinary(bin, c.GOOS, c.GOARCH); err != nil {
		return "", err
	}
	if err := replace(exe, bin); err != nil {
		return "", err
	}
	return tag, nil
}

func (c *Client) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", filepath.Base(u), res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", filepath.Base(u), err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("downloading %s: larger than expected", filepath.Base(u))
	}
	return b, nil
}

// checksumFor finds the SHA-256 of name in a sha256sum listing.
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if len(f[0]) != 64 {
				break
			}
			if _, err := hex.DecodeString(f[0]); err != nil {
				break
			}
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in the release", name)
}

// extract returns the regular file called name from a .tar.gz or a .zip.
func extract(archive []byte, name string) ([]byte, error) {
	if bytes.HasPrefix(archive, []byte("PK\x03\x04")) {
		return extractZip(archive, name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s is not in the release archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Name != name || h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Size > maxBinary {
			return nil, errors.New("the binary in the archive is larger than expected")
		}
		return io.ReadAll(io.LimitReader(tr, maxBinary))
	}
}

// extractZip returns the regular file called name from a .zip.
func extractZip(archive []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name != name || !f.Mode().IsRegular() {
			continue
		}
		if f.UncompressedSize64 > maxBinary {
			return nil, errors.New("the binary in the archive is larger than expected")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		b, err := io.ReadAll(io.LimitReader(rc, maxBinary+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxBinary {
			return nil, errors.New("the binary in the archive is larger than expected")
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s is not in the release archive", name)
}

// replace writes bin next to exe and swaps it in.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".lazycompose-update-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			if runtime.GOOS == "windows" {
				return fmt.Errorf("no permission to write to %s; run it from a terminal opened as administrator", dir)
			}
			return fmt.Errorf("no permission to write to %s; run it with sudo: sudo %s -update", dir, exe)
		}
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := swap(tmp.Name(), exe); err != nil {
		return err
	}
	done = true
	return nil
}
