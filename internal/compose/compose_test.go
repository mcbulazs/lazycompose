package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePSLines(t *testing.T) {
	cs, err := ParsePS(fixture(t, "ps_ndjson.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d containers, want 3", len(cs))
	}

	tests := []struct {
		service, state, health, ports, uptime string
		running                               bool
	}{
		{"db", "running", "healthy", "", "4 days", true},
		{"web", "running", "", "8080->80/tcp, 443/tcp", "3 hours", true},
		{"worker", "exited", "", "", "", false},
	}
	for i, tt := range tests {
		c := cs[i]
		if c.Service != tt.service {
			t.Errorf("%d: service %q, want %q", i, c.Service, tt.service)
		}
		if c.State != tt.state {
			t.Errorf("%s: state %q, want %q", c.Service, c.State, tt.state)
		}
		if c.Health != tt.health {
			t.Errorf("%s: health %q, want %q", c.Service, c.Health, tt.health)
		}
		if got := c.Ports(); got != tt.ports {
			t.Errorf("%s: ports %q, want %q", c.Service, got, tt.ports)
		}
		if got := c.Uptime(); got != tt.uptime {
			t.Errorf("%s: uptime %q, want %q", c.Service, got, tt.uptime)
		}
		if c.Running() != tt.running {
			t.Errorf("%s: running %v, want %v", c.Service, c.Running(), tt.running)
		}
	}
	if cs[2].ExitCode != 137 {
		t.Errorf("worker exit code %d, want 137", cs[2].ExitCode)
	}
}

func TestParsePSArray(t *testing.T) {
	cs, err := ParsePS(fixture(t, "ps_array.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d containers, want 1", len(cs))
	}
	c := cs[0]
	if c.Service != "server" || c.Health != "starting" {
		t.Errorf("unexpected container: %+v", c)
	}
	if got := c.Uptime(); got != "Less than a second" {
		t.Errorf("uptime %q", got)
	}
	if got := c.Ports(); got != "25565/tcp" {
		t.Errorf("ports %q", got)
	}
}

func TestParsePSEmpty(t *testing.T) {
	cs, err := ParsePS(fixture(t, "ps_empty.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected no containers, got %d", len(cs))
	}
}

func TestParsePSInvalid(t *testing.T) {
	if _, err := ParsePS([]byte(`{"Name": `)); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDockerPS(t *testing.T) {
	m, err := ParseDockerPS(fixture(t, "docker_ps.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// the working_dir labels are cleaned, so on Windows they have \
	b := m[filepath.FromSlash("/srv/stacks/web/blog")]
	if b == nil {
		t.Fatalf("blog missing: %v", m)
	}
	// the one-off "run" container is ignored
	if b.Running != 2 || b.Total != 3 {
		t.Errorf("blog running=%d total=%d, want 2/3", b.Running, b.Total)
	}
	if !b.Services["db"] || !b.Services["web"] || b.Services["worker"] {
		t.Errorf("blog services %v", b.Services)
	}

	p := m["/srv/stacks/jellyfin"]
	if p == nil || p.Running != 0 || p.Total != 1 {
		t.Errorf("jellyfin state %+v", p)
	}
}

func TestParseLabelsWithCommas(t *testing.T) {
	m := parseLabels("a=1,com.docker.compose.project.config_files=/x/compose.yaml,/x/override.yaml,b=2")
	if m["com.docker.compose.project.config_files"] != "/x/compose.yaml,/x/override.yaml" {
		t.Errorf("config_files %q", m["com.docker.compose.project.config_files"])
	}
	if m["a"] != "1" || m["b"] != "2" {
		t.Errorf("labels %v", m)
	}
}

func TestShort(t *testing.T) {
	tests := map[string]string{
		"postgres:16-alpine":                  "postgres:16-alpine",
		"ghcr.io/example/blog:latest":         "blog:latest",
		"lscr.io/linuxserver/jellyfin:1.40":   "jellyfin:1.40",
		"sha256:b92841763c5c2140a20eeb2a44e9": "sha256:b92841763c5c",
	}
	for in, want := range tests {
		if got := Short(in); got != want {
			t.Errorf("Short(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDockerPSProblems(t *testing.T) {
	m, err := ParseDockerPS(fixture(t, "docker_ps.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"/srv/stacks/sick":     1, // unhealthy
		"/srv/stacks/loop":     1, // restarting
		"/srv/stacks/crashed":  1, // worker exited with 2
		"/srv/stacks/stopped":  0, // killed by a stop, not a crash
		"/srv/stacks/old":      0, // stopped, health status left over
		"/srv/stacks/web/blog": 0, // worker exited with 137
		"/srv/stacks/jellyfin": 0,
	}
	for dir, n := range want {
		st := m[filepath.FromSlash(dir)]
		if st == nil {
			t.Errorf("%s missing", dir)
			continue
		}
		if st.Problems != n {
			t.Errorf("%s: %d problems, want %d", dir, st.Problems, n)
		}
	}
}

func TestContainerProblem(t *testing.T) {
	tests := []struct {
		c    Container
		want bool
	}{
		{Container{State: "running", Health: "healthy"}, false},
		{Container{State: "running", Health: "unhealthy"}, true},
		{Container{State: "restarting"}, true},
		{Container{State: "exited", ExitCode: 0}, false},
		{Container{State: "exited", ExitCode: 1}, true},
		{Container{State: "exited", ExitCode: 137}, false},
		{Container{State: "exited", ExitCode: 143}, false},
		{Container{State: "exited", Health: "unhealthy"}, false},
	}
	for _, tt := range tests {
		if got := tt.c.Problem(); got != tt.want {
			t.Errorf("%+v: got %v, want %v", tt.c, got, tt.want)
		}
	}
}

func TestParseStats(t *testing.T) {
	m, err := ParseStats(fixture(t, "stats.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Stats{
		"blog-web-1": {CPU: "0.15%", Mem: "52.3MiB"},
		"blog-db-1":  {CPU: "103.40%", Mem: "1.084GiB"},
	}
	if len(m) != len(want) {
		t.Fatalf("got %d entries, want %d", len(m), len(want))
	}
	for name, w := range want {
		if m[name] != w {
			t.Errorf("%s: got %+v, want %+v", name, m[name], w)
		}
	}
}

func TestParseServiceImages(t *testing.T) {
	inspect := "sha256:aaa\tnginx:alpine\t/srv/web\tproxy\n" +
		"sha256:bbb\tmyapp\t/srv/app\tweb\n" +
		"sha256:aaa\tnginx:alpine\t/srv/other\tproxy\n" +
		"sha256:ccc\tbusybox\t\t\n" // not from compose
	images, ids := ParseContainerImages([]byte(inspect))
	if len(images) != 3 || len(ids) != 2 {
		t.Fatalf("got %d images and ids %v", len(images), ids)
	}
	if images[0].Dir != "/srv/web" || images[0].Service != "proxy" || images[0].Ref != "nginx:alpine" {
		t.Errorf("first image = %+v", images[0])
	}

	digests := ParseRepoDigests([]byte("sha256:aaa\t[\"nginx@sha256:111\",\"mirror.local/nginx@sha256:222\"]\n" +
		"sha256:bbb\t[]\n"))
	if got := digests["sha256:aaa"]; len(got) != 2 || got[0] != "sha256:111" || got[1] != "sha256:222" {
		t.Errorf("digests of aaa = %v", got)
	}
	if got := digests["sha256:bbb"]; len(got) != 0 {
		t.Errorf("a locally built image should have no digests, got %v", got)
	}
}

func TestParseOldImages(t *testing.T) {
	out := "sha256:aaa\t[\"nginx@sha256:111\"]\t45000000\t2026-10-01T10:00:00.123Z\n" +
		"sha256:bbb\t[]\t1000\t2026-09-01T10:00:00Z\n" +
		"sha256:ccc\t[\"redis@sha256:222\"]\t9000\t2026-08-01T10:00:00Z\n" +
		"sha256:ddd\t[\"nginx@sha256:333\"]\t44000000\t2026-10-20T10:00:00Z\n" +
		"Error: No such image: sha256:eee\n"
	got := ParseOldImages([]byte(out), map[string]bool{"sha256:ccc": true})
	var ids []string
	for _, img := range got {
		ids = append(ids, img.ID)
	}
	// unknown repo first, then by repo, newest first; used ones left out
	if strings.Join(ids, ",") != "sha256:bbb,sha256:ddd,sha256:aaa" {
		t.Fatalf("ids = %v", ids)
	}
	if got[1].Repo != "nginx" || got[1].Size != 44000000 || got[1].Created.Day() != 20 {
		t.Errorf("parsed %+v", got[1])
	}
}
