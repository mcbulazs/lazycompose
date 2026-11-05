// Package registry asks image registries for the digest an image tag points
// to, without pulling it. Only anonymous access is used, so images from
// private registries are reported as errors and skipped by the caller.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Ref is an image reference split into its parts.
type Ref struct {
	Host, Repo, Tag string
}

// ParseRef splits an image reference like "nginx", "user/app:1.2" or
// "ghcr.io/org/app:latest". ok is false for references pinned to a digest,
// which cannot have updates.
func ParseRef(s string) (Ref, bool) {
	if s == "" || strings.Contains(s, "@") {
		return Ref{}, false
	}
	host, rest := "docker.io", s
	if i := strings.IndexByte(s, '/'); i >= 0 {
		first := s[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			host, rest = first, s[i+1:]
		}
	}
	tag := "latest"
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		rest, tag = rest[:i], rest[i+1:]
	}
	if host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io" {
		host = "registry-1.docker.io"
		if !strings.Contains(rest, "/") {
			rest = "library/" + rest
		}
	}
	if rest == "" || tag == "" {
		return Ref{}, false
	}
	return Ref{Host: host, Repo: strings.ToLower(rest), Tag: tag}, true
}

var accept = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
}, ", ")

// maxBody limits what is read from a registry response.
const maxBody = 4 << 20

type Client struct {
	HTTP   *http.Client
	scheme string // "https", or "http" in tests
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, scheme: "https"}
}

// Digest returns the digest the tag of ref points to, like "sha256:...".
// It uses a HEAD request, which Docker Hub does not count as a pull.
func (c *Client) Digest(ctx context.Context, ref Ref) (string, error) {
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.scheme, ref.Host, ref.Repo, url.PathEscape(ref.Tag))
	token := ""
	for attempt := 0; ; attempt++ {
		res, err := c.do(ctx, http.MethodHead, u, token)
		if err != nil {
			return "", err
		}
		_ = res.Body.Close() // HEAD has no body
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			token, err = c.token(ctx, res.Header.Get("Www-Authenticate"))
			if err != nil {
				return "", err
			}
			continue
		}
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("%s/%s:%s: %s", ref.Host, ref.Repo, ref.Tag, res.Status)
		}
		if d := res.Header.Get("Docker-Content-Digest"); d != "" {
			return d, nil
		}
		// some registries only send the digest with the body
		return c.digestFromBody(ctx, u, token)
	}
}

func (c *Client) do(ctx context.Context, method, u, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.HTTP.Do(req)
}

func (c *Client) digestFromBody(ctx context.Context, u, token string) (string, error) {
	res, err := c.do(ctx, http.MethodGet, u, token)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New(res.Status)
	}
	if d := res.Header.Get("Docker-Content-Digest"); d != "" {
		return d, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(res.Body, maxBody)); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// token gets an anonymous pull token for a Bearer challenge like
// `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`.
func (c *Client) token(ctx context.Context, challenge string) (string, error) {
	scheme, params, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "bearer") {
		return "", errors.New("registry needs a login")
	}
	p := parseParams(params)
	realm, err := url.Parse(p["realm"])
	if err != nil || realm.Host == "" || realm.Scheme != c.scheme {
		return "", fmt.Errorf("bad auth realm %q", p["realm"])
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if p[k] != "" {
			q.Set(k, p[k])
		}
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token: %s", res.Status)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&body); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", errors.New("token: empty response")
}

// parseParams reads key="value" pairs separated by commas.
func parseParams(s string) map[string]string {
	out := map[string]string{}
	for s != "" {
		s = strings.TrimLeft(s, " ,")
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		var v string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				v, rest = rest[1:], ""
			} else {
				v, rest = rest[1:end+1], rest[end+2:]
			}
		} else {
			v, rest, _ = strings.Cut(rest, ",")
		}
		out[strings.ToLower(strings.TrimSpace(k))] = v
		s = rest
	}
	return out
}
