package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in   string
		want Ref
		ok   bool
	}{
		{"nginx", Ref{"registry-1.docker.io", "library/nginx", "latest"}, true},
		{"nginx:1.27-alpine", Ref{"registry-1.docker.io", "library/nginx", "1.27-alpine"}, true},
		{"docker.io/jellyfin/jellyfin", Ref{"registry-1.docker.io", "jellyfin/jellyfin", "latest"}, true},
		{"linuxserver/sonarr:4", Ref{"registry-1.docker.io", "linuxserver/sonarr", "4"}, true},
		{"ghcr.io/home-assistant/home-assistant:stable", Ref{"ghcr.io", "home-assistant/home-assistant", "stable"}, true},
		{"localhost:5000/app", Ref{"localhost:5000", "app", "latest"}, true},
		{"registry.local:5000/team/app:v2", Ref{"registry.local:5000", "team/app", "v2"}, true},
		{"nginx@sha256:abc", Ref{}, false},
		{"nginx:1@sha256:abc", Ref{}, false},
		{"", Ref{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseRef(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseRef(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseParams(t *testing.T) {
	got := parseParams(`realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)
	if got["realm"] != "https://auth.docker.io/token" || got["service"] != "registry.docker.io" || got["scope"] != "repository:library/nginx:pull" {
		t.Errorf("parseParams = %v", got)
	}
}

// fakeRegistry needs a token like Docker Hub and answers HEAD with the digest.
func fakeRegistry(t *testing.T, digestHeader bool) (*httptest.Server, *int) {
	heads := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:library/nginx:pull" {
				t.Errorf("token scope = %q", r.URL.Query().Get("scope"))
			}
			fmt.Fprint(w, `{"token":"secret"}`)
		case r.URL.Path == "/v2/library/nginx/manifests/latest":
			if r.Header.Get("Authorization") != "Bearer secret" {
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test",scope="repository:library/nginx:pull"`, srv.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !strings.Contains(r.Header.Get("Accept"), "image.index") {
				t.Error("should accept multi-arch indexes")
			}
			if r.Method == http.MethodHead {
				heads++
			}
			if digestHeader {
				w.Header().Set("Docker-Content-Digest", "sha256:1234")
			}
			fmt.Fprint(w, `{"manifest":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &heads
}

func testClient(srv *httptest.Server) (*Client, string) {
	return &Client{HTTP: srv.Client(), scheme: "http"}, strings.TrimPrefix(srv.URL, "http://")
}

func TestDigest(t *testing.T) {
	srv, heads := fakeRegistry(t, true)
	c, host := testClient(srv)
	d, err := c.Digest(context.Background(), Ref{host, "library/nginx", "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if d != "sha256:1234" {
		t.Errorf("digest = %q", d)
	}
	if *heads != 1 {
		t.Errorf("made %d authorised HEAD requests, want 1", *heads)
	}
}

func TestDigestFromBody(t *testing.T) {
	srv, _ := fakeRegistry(t, false)
	c, host := testClient(srv)
	d, err := c.Digest(context.Background(), Ref{host, "library/nginx", "latest"})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(`{"manifest":true}`))
	if want := "sha256:" + hex.EncodeToString(sum[:]); d != want {
		t.Errorf("digest = %q, want %q", d, want)
	}
}

func TestDigestErrors(t *testing.T) {
	srv, _ := fakeRegistry(t, true)
	c, host := testClient(srv)
	if _, err := c.Digest(context.Background(), Ref{host, "library/missing", "latest"}); err == nil {
		t.Error("missing image should fail")
	}

	// a realm on another scheme is refused, so a token is never sent to it
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", `Bearer realm="file:///etc/passwd"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	c, host = testClient(bad)
	if _, err := c.Digest(context.Background(), Ref{host, "x", "latest"}); err == nil {
		t.Error("bad realm should fail")
	}
}
