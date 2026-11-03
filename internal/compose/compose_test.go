package compose

import (
	"os"
	"path/filepath"
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

	b := m["/mnt/data/global/web/blogger"]
	if b == nil {
		t.Fatalf("blogger missing: %v", m)
	}
	// the one-off "run" container is ignored
	if b.Running != 2 || b.Total != 3 {
		t.Errorf("blogger running=%d total=%d, want 2/3", b.Running, b.Total)
	}
	if !b.Services["db"] || !b.Services["web"] || b.Services["worker"] {
		t.Errorf("blogger services %v", b.Services)
	}

	p := m["/mnt/data/global/plex"]
	if p == nil || p.Running != 0 || p.Total != 1 {
		t.Errorf("plex state %+v", p)
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
		"ghcr.io/mcbulazs/blogger:latest":     "blogger:latest",
		"lscr.io/linuxserver/plex:1.40":       "plex:1.40",
		"sha256:b92841763c5c2140a20eeb2a44e9": "sha256:b92841763c5c",
	}
	for in, want := range tests {
		if got := Short(in); got != want {
			t.Errorf("Short(%q) = %q, want %q", in, got, want)
		}
	}
}
