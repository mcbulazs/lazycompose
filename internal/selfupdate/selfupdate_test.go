package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarball(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {name, data}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHub serves a latest release tag, its archive and checksums.
func fakeGitHub(t *testing.T, tag string, archive []byte, sum string) *Client {
	t.Helper()
	asset := "lazycompose_linux_amd64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/latest":
			http.Redirect(w, r, "/"+Repo+"/releases/tag/"+tag, http.StatusFound)
		case "/" + Repo + "/releases/download/" + tag + "/checksums.txt":
			fmt.Fprintf(w, "%s  lazycompose_linux_arm64.tar.gz\n%s  %s\n", strings.Repeat("0", 64), sum, asset)
		case "/" + Repo + "/releases/download/" + tag + "/" + asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New()
	c.Base = srv.URL
	c.GOOS, c.GOARCH = "linux", "amd64"
	return c
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.3.1", "v0.3.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.2.9", "v0.3.0", false},
		{"v0.4.0", "dev", false},
		{"v0.4.0", "v0.3.1-0.20261105120000-abcdef+dirty", false},
		{"v0.4.0-rc1", "v0.3.0", false},
	} {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.latest, tc.current, got)
		}
	}
}

func TestUpdate(t *testing.T) {
	bin := []byte("\x7fELF new version")
	archive := tarball(t, "lazycompose", bin)
	c := fakeGitHub(t, "v0.4.0", archive, sha(archive))

	if tag, err := c.Latest(context.Background()); err != nil || tag != "v0.4.0" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}

	exe := filepath.Join(t.TempDir(), "lazycompose")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	tag, err := c.Update(context.Background(), "v0.3.0", exe)
	if err != nil || tag != "v0.4.0" {
		t.Fatalf("Update = %q, %v", tag, err)
	}
	got, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	if !bytes.Equal(got, bin) || info.Mode().Perm() != 0o755 {
		t.Errorf("binary = %q, mode %v", got, info.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".lazycompose-update-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}

	if _, err := c.Update(context.Background(), "v0.4.0", exe); !errors.Is(err, ErrUpToDate) {
		t.Errorf("same version should be up to date, got %v", err)
	}
	if _, err := c.Update(context.Background(), "dev", exe); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Errorf("a development build should not update, got %v", err)
	}
	if _, err := c.Update(context.Background(), "v0.3.0", "/usr/bin/lazycompose"); err == nil || !strings.Contains(err.Error(), "package manager") {
		t.Errorf("a packaged binary should not update, got %v", err)
	}
}

func TestManaged(t *testing.T) {
	for exe, want := range map[string]bool{
		"/usr/bin/lazycompose":                       true,
		"/bin/lazycompose":                           true,
		"/nix/store/abc-lazycompose/bin/lazycompose": true,
		"/usr/local/bin/lazycompose":                 false,
		"/home/me/.local/bin/lazycompose":            false,
		"/usr/bin/sub/lazycompose":                   false,
	} {
		if got := Managed(exe); got != want {
			t.Errorf("Managed(%q) = %v, want %v", exe, got, want)
		}
	}
}

func TestUpdateRefusesBadDownloads(t *testing.T) {
	good := tarball(t, "lazycompose", []byte("\x7fELF new"))
	notELF := tarball(t, "lazycompose", []byte("#!/bin/sh\necho hi\n"))
	noBinary := tarball(t, "other", []byte("\x7fELF"))
	for name, c := range map[string]*Client{
		"checksum":      fakeGitHub(t, "v0.4.0", good, strings.Repeat("a", 64)),
		"not a program": fakeGitHub(t, "v0.4.0", notELF, sha(notELF)),
		"missing":       fakeGitHub(t, "v0.4.0", noBinary, sha(noBinary)),
	} {
		exe := filepath.Join(t.TempDir(), "lazycompose")
		if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Update(context.Background(), "v0.3.0", exe); err == nil {
			t.Errorf("%s: should refuse", name)
		}
		if got, _ := os.ReadFile(exe); string(got) != "old" {
			t.Errorf("%s: the old binary should stay, got %q", name, got)
		}
	}
}

func TestRedirectsOnlyToGitHub(t *testing.T) {
	c := New()
	evil := httptest.NewRequest(http.MethodGet, "https://evil.example/x", nil)
	if err := c.checkRedirect(evil, nil); err == nil {
		t.Error("a redirect to another host should be refused")
	}
	plain := httptest.NewRequest(http.MethodGet, "http://objects.githubusercontent.com/x", nil)
	if err := c.checkRedirect(plain, nil); err == nil {
		t.Error("a redirect to plain http should be refused")
	}
	ok := httptest.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/x", nil)
	if err := c.checkRedirect(ok, nil); err != nil {
		t.Errorf("GitHub's download host should be allowed: %v", err)
	}
}

func TestAssetName(t *testing.T) {
	c := &Client{GOOS: "darwin", GOARCH: "arm64"}
	if _, err := c.AssetName(); err == nil {
		t.Error("there are no macOS releases")
	}
}
