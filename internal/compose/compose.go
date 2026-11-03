package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Project runs docker compose for a single compose file.
type Project struct {
	Dir  string
	File string
}

// Command builds a docker compose command for the project. It is also used
// for interactive commands handed to the terminal.
func (p Project) Command(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"compose", "-f", p.File}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = p.Dir
	return cmd
}

// Run runs a command and returns its combined output.
func (p Project) Run(ctx context.Context, args ...string) (string, error) {
	out, err := p.Command(ctx, args...).CombinedOutput()
	return string(out), wrap(err, out)
}

// PS returns all containers of the project, including stopped ones.
func (p Project) PS(ctx context.Context) ([]Container, error) {
	cmd := p.Command(ctx, "ps", "-a", "--format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, wrap(err, stderr.Bytes())
	}
	return ParsePS(out)
}

// Services lists the service names defined in the compose file.
func (p Project) Services(ctx context.Context) ([]string, error) {
	cmd := p.Command(ctx, "config", "--services")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, wrap(err, stderr.Bytes())
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Validate runs `config -q` and returns its output as the error on failure.
func (p Project) Validate(ctx context.Context) error {
	_, err := p.Run(ctx, "config", "-q")
	return err
}

func wrap(err error, out []byte) error {
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return err
}

type Publisher struct {
	URL           string `json:"URL"`
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

// Container is one entry of `docker compose ps --format json`.
type Container struct {
	ID         string      `json:"ID"`
	Name       string      `json:"Name"`
	Service    string      `json:"Service"`
	Project    string      `json:"Project"`
	Image      string      `json:"Image"`
	State      string      `json:"State"`
	Health     string      `json:"Health"`
	Status     string      `json:"Status"`
	ExitCode   int         `json:"ExitCode"`
	Publishers []Publisher `json:"Publishers"`
}

func (c Container) Running() bool { return c.State == "running" }

// Problem reports an unhealthy, crash looping or crashed container.
func (c Container) Problem() bool { return problem(c.State, c.Health, c.ExitCode) }

// problem is true for unhealthy, restarting or dead containers, and for
// ones that exited with an error. 137 and 143 are what a normal stop or
// kill leaves behind, so they do not count. Docker keeps the last health
// status after a container stops, so health only counts while running.
func problem(state, health string, exitCode int) bool {
	switch {
	case state == "running" && health == "unhealthy", state == "restarting", state == "dead":
		return true
	case state == "exited":
		return exitCode != 0 && exitCode != 137 && exitCode != 143
	}
	return false
}

var exitedRe = regexp.MustCompile(`^Exited \((-?\d+)\)`)

// exitCodeFromStatus reads the code from a status like "Exited (2) 1 hour ago".
func exitCodeFromStatus(status string) int {
	if m := exitedRe.FindStringSubmatch(status); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// Ports formats published ports, merging the IPv4 and IPv6 bindings.
func (c Container) Ports() string {
	seen := map[string]bool{}
	var out []string
	for _, p := range c.Publishers {
		if p.PublishedPort == 0 {
			continue
		}
		s := fmt.Sprintf("%d->%d/%s", p.PublishedPort, p.TargetPort, p.Protocol)
		if p.PublishedPort == p.TargetPort {
			s = fmt.Sprintf("%d/%s", p.PublishedPort, p.Protocol)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

// Uptime extracts the duration from a status like "Up 2 hours (healthy)".
func (c Container) Uptime() string {
	if !c.Running() || !strings.HasPrefix(c.Status, "Up ") {
		return ""
	}
	s := strings.TrimPrefix(c.Status, "Up ")
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	return s
}

// ParsePS accepts both the newline-delimited output of current compose
// versions and the single JSON array printed by older ones.
func ParsePS(data []byte) ([]Container, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	var cs []Container
	if data[0] == '[' {
		if err := json.Unmarshal(data, &cs); err != nil {
			return nil, err
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader(data))
		for dec.More() {
			var c Container
			if err := dec.Decode(&c); err != nil {
				return nil, err
			}
			cs = append(cs, c)
		}
	}

	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Service != cs[j].Service {
			return cs[i].Service < cs[j].Service
		}
		return cs[i].Name < cs[j].Name
	})
	return cs, nil
}

// DirState is the container count for one compose working directory.
type DirState struct {
	Running  int
	Total    int
	Services map[string]bool // services with a running container
	Problems int             // unhealthy, crash looping or crashed containers
}

// StateByDir reads every compose-managed container on the host with a single
// `docker ps` call and groups them by project working directory.
func StateByDir(ctx context.Context) (map[string]*DirState, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=com.docker.compose.project", "--format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, wrap(err, stderr.Bytes())
	}
	return ParseDockerPS(out)
}

const (
	labelWorkingDir = "com.docker.compose.project.working_dir"
	labelService    = "com.docker.compose.service"
	labelOneoff     = "com.docker.compose.oneoff"
)

// ParseDockerPS parses `docker ps --format json` output.
func ParseDockerPS(data []byte) (map[string]*DirState, error) {
	res := map[string]*DirState{}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	for dec.More() {
		var row struct {
			State  string `json:"State"`
			Status string `json:"Status"`
			Health string `json:"HealthStatus"`
			Labels string `json:"Labels"`
		}
		if err := dec.Decode(&row); err != nil {
			return nil, err
		}
		labels := parseLabels(row.Labels)
		dir := labels[labelWorkingDir]
		if dir == "" || strings.EqualFold(labels[labelOneoff], "true") {
			continue
		}
		dir = filepath.Clean(dir)
		st := res[dir]
		if st == nil {
			st = &DirState{Services: map[string]bool{}}
			res[dir] = st
		}
		st.Total++
		if row.State == "running" {
			st.Running++
			st.Services[labels[labelService]] = true
		}
		if problem(row.State, row.Health, exitCodeFromStatus(row.Status)) {
			st.Problems++
		}
	}
	return res, nil
}

// parseLabels splits "a=1,b=2". Values may contain commas (config_files can),
// so a part without "=" is joined to the previous value.
func parseLabels(s string) map[string]string {
	m := map[string]string{}
	last := ""
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.ContainsAny(k, " /") {
			if last != "" {
				m[last] += "," + part
			}
			continue
		}
		m[k] = v
		last = k
	}
	return m
}

// Short trims an image reference for display.
func Short(image string) string {
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	if strings.HasPrefix(image, "sha256:") && len(image) > 19 {
		image = image[:19]
	}
	return image
}
