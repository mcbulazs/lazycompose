// Package selfupdate finds the latest lazycompose release on GitHub and
// replaces the running binary with it.
package selfupdate

import (
	"archive/tar"
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

// AssetName is the release archive for this system.
func (c *Client) AssetName() (string, error) {
	if c.GOOS != "linux" || (c.GOARCH != "amd64" && c.GOARCH != "arm64") {
		return "", fmt.Errorf("there are release binaries for linux amd64 and arm64 only, not %s %s", c.GOOS, c.GOARCH)
	}
	return fmt.Sprintf("lazycompose_%s_%s.tar.gz", c.GOOS, c.GOARCH), nil
}

// Update replaces the binary at exe with the latest release if it is newer
// than current, and returns the new version. The archive is checked against
// the release's checksums, and the binary is swapped in with a rename, so a
// failure leaves the old binary in place.
func (c *Client) Update(ctx context.Context, current, exe string) (string, error) {
	if !IsRelease(current) {
		return "", fmt.Errorf("this is a development build (%s), not a release; install a release first, see the README", current)
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
	bin, err := extract(archive, "lazycompose")
	if err != nil {
		return "", err
	}
	if !bytes.HasPrefix(bin, []byte("\x7fELF")) {
		return "", errors.New("the downloaded file is not a Linux program, not installing it")
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

// extract returns the regular file called name from a .tar.gz.
func extract(archive []byte, name string) ([]byte, error) {
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

// replace writes bin next to exe and renames it over exe.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".lazycompose-update-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
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
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return err
	}
	done = true
	return nil
}
