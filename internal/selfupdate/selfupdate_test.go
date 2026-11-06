package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBin is the start of a program for goos and goarch: enough of its
// header for checkBinary, then some text.
func fakeBin(goos, goarch, text string) []byte {
	le := binary.LittleEndian
	var b []byte
	switch goos {
	case "linux":
		b = make([]byte, 64)
		copy(b, "\x7fELF")
		le.PutUint16(b[18:], map[string]uint16{"amd64": 62, "arm64": 183}[goarch])
	case "darwin":
		b = make([]byte, 32)
		le.PutUint32(b, 0xfeedfacf)
		le.PutUint32(b[4:], map[string]uint32{"amd64": 0x01000007, "arm64": 0x0100000c}[goarch])
	case "windows":
		b = make([]byte, 0x80+24)
		copy(b, "MZ")
		le.PutUint32(b[0x3c:], 0x80)
		copy(b[0x80:], "PE\x00\x00")
		le.PutUint16(b[0x84:], map[string]uint16{"amd64": 0x8664, "arm64": 0xaa64}[goarch])
	}
	return append(b, text...)
}

func zipball(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {name, data}} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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

// fakeGitHub serves a latest release tag, its linux amd64 archive and
// checksums.
func fakeGitHub(t *testing.T, tag string, archive []byte, sum string) *Client {
	t.Helper()
	return fakeGitHubFor(t, "linux", "amd64", tag, archive, sum)
}

func fakeGitHubFor(t *testing.T, goos, goarch, tag string, archive []byte, sum string) *Client {
	t.Helper()
	c := &Client{GOOS: goos, GOARCH: goarch}
	asset, err := c.AssetName()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/latest":
			http.Redirect(w, r, "/"+Repo+"/releases/tag/"+tag, http.StatusFound)
		case "/" + Repo + "/releases/download/" + tag + "/checksums.txt":
			fmt.Fprintf(w, "%s  lazycompose_other.tar.gz\n%s  %s\n", strings.Repeat("0", 64), sum, asset)
		case "/" + Repo + "/releases/download/" + tag + "/" + asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c = New()
	c.Base = srv.URL
	c.GOOS, c.GOARCH = goos, goarch
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
	bin := fakeBin("linux", "amd64", "new version")
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
	if !bytes.Equal(got, bin) || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
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

func TestUpdateOtherSystems(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, name string }{
		{"darwin", "arm64", "lazycompose"},
		{"darwin", "amd64", "lazycompose"},
		{"windows", "amd64", "lazycompose.exe"},
		{"windows", "arm64", "lazycompose.exe"},
		{"linux", "arm64", "lazycompose"},
	} {
		bin := fakeBin(tc.goos, tc.goarch, "new")
		archive := tarball(t, tc.name, bin)
		if tc.goos == "windows" {
			archive = zipball(t, tc.name, bin)
		}
		c := fakeGitHubFor(t, tc.goos, tc.goarch, "v0.5.0", archive, sha(archive))
		exe := filepath.Join(t.TempDir(), tc.name)
		if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Update(context.Background(), "v0.4.0", exe); err != nil {
			t.Errorf("%s %s: %v", tc.goos, tc.goarch, err)
			continue
		}
		if got, _ := os.ReadFile(exe); !bytes.Equal(got, bin) {
			t.Errorf("%s %s: binary not replaced", tc.goos, tc.goarch)
		}
	}
}

func TestCheckBinary(t *testing.T) {
	for _, tc := range []struct {
		bin          []byte
		goos, goarch string
		want         string // part of the error, "" for none
	}{
		{fakeBin("linux", "amd64", ""), "linux", "amd64", ""},
		{fakeBin("darwin", "arm64", ""), "darwin", "arm64", ""},
		{fakeBin("windows", "arm64", ""), "windows", "arm64", ""},
		{fakeBin("linux", "arm64", ""), "linux", "amd64", "is for arm64, not amd64"},
		{fakeBin("darwin", "amd64", ""), "darwin", "arm64", "is for amd64, not arm64"},
		{fakeBin("linux", "amd64", ""), "darwin", "amd64", "not a macOS program"},
		{fakeBin("darwin", "amd64", ""), "windows", "amd64", "not a Windows program"},
		{[]byte("#!/bin/sh\necho hi\n"), "linux", "amd64", "not a Linux program"},
		{[]byte("MZ"), "windows", "amd64", "not a Windows program"},
		{append([]byte("MZ"), make([]byte, 0x40)...), "windows", "amd64", "not a Windows program"},
	} {
		err := checkBinary(tc.bin, tc.goos, tc.goarch)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s %s: %v", tc.goos, tc.goarch, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s %s: got %v, want %q", tc.goos, tc.goarch, err, tc.want)
		}
	}
}

func TestManaged(t *testing.T) {
	for exe, want := range map[string]bool{
		"/usr/bin/lazycompose":                                                                     true,
		"/bin/lazycompose":                                                                         true,
		"/nix/store/abc-lazycompose/bin/lazycompose":                                               true,
		"/usr/local/bin/lazycompose":                                                               false,
		"/home/me/.local/bin/lazycompose":                                                          false,
		"/usr/bin/sub/lazycompose":                                                                 false,
		"/opt/homebrew/Cellar/lazycompose/0.5.0/bin/lazycompose":                                   true,
		"/usr/local/Cellar/lazycompose/0.5.0/bin/lazycompose":                                      true,
		"/home/linuxbrew/.linuxbrew/Cellar/lazycompose/0.5.0/bin/lc":                               true,
		`C:\Users\me\scoop\apps\lazycompose\current\lazycompose.exe`:                               true,
		`C:\Users\me\AppData\Local\Microsoft\WinGet\Packages\mcbulazs.lazycompose\lazycompose.exe`: true,
		`C:\Users\me\AppData\Local\Programs\lazycompose\lazycompose.exe`:                           false,
	} {
		if got := Managed(exe); got != want {
			t.Errorf("Managed(%q) = %v, want %v", exe, got, want)
		}
	}
}

func TestUpdateRefusesBadDownloads(t *testing.T) {
	good := tarball(t, "lazycompose", fakeBin("linux", "amd64", "new"))
	notELF := tarball(t, "lazycompose", []byte("#!/bin/sh\necho hi\n"))
	otherArch := tarball(t, "lazycompose", fakeBin("linux", "arm64", "new"))
	noBinary := tarball(t, "other", fakeBin("linux", "amd64", ""))
	for name, c := range map[string]*Client{
		"checksum":      fakeGitHub(t, "v0.4.0", good, strings.Repeat("a", 64)),
		"not a program": fakeGitHub(t, "v0.4.0", notELF, sha(notELF)),
		"other arch":    fakeGitHub(t, "v0.4.0", otherArch, sha(otherArch)),
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
	for sys, want := range map[[2]string]string{
		{"linux", "amd64"}:   "lazycompose_linux_amd64.tar.gz",
		{"darwin", "arm64"}:  "lazycompose_darwin_arm64.tar.gz",
		{"windows", "amd64"}: "lazycompose_windows_amd64.zip",
		{"freebsd", "amd64"}: "",
		{"linux", "386"}:     "",
	} {
		c := &Client{GOOS: sys[0], GOARCH: sys[1]}
		got, err := c.AssetName()
		if want == "" && err == nil {
			t.Errorf("%v: got %q, want no release", sys, got)
		}
		if want != "" && got != want {
			t.Errorf("%v: got %q, %v, want %q", sys, got, err, want)
		}
	}
}
