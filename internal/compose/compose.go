package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mcbulazs/lazycompose/internal/remote"
)

// Project runs docker compose for a single compose file.
type Project struct {
	Dir  string
	File string
	// Host is the machine the project is on; nil is this one. Dir and File
	// are paths on that machine.
	Host *remote.Host
}

// Command builds a docker compose command for the project. It is also used
// for interactive commands handed to the terminal.
func (p Project) Command(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"compose", "-f", p.File}, args...)
	if p.Host != nil {
		// compose takes the project folder from the file, so there is no
		// need to cd there first
		return Docker(ctx, p.Host, full...)
	}
	cmd := Docker(ctx, nil, full...)
	cmd.Dir = p.Dir
	return cmd
}

// Terminal is Command for a command the user works in, like a shell in a
// container: over ssh it gets a terminal.
func (p Project) Terminal(args ...string) *exec.Cmd {
	if p.Host == nil {
		return p.Command(context.Background(), args...)
	}
	return p.Host.TerminalCommand(append([]string{"docker", "compose", "-f", p.File}, args...)...)
}

// Docker runs docker on the host: here when it is nil, otherwise over ssh.
func Docker(ctx context.Context, h *remote.Host, args ...string) *exec.Cmd {
	if h == nil {
		return exec.CommandContext(ctx, "docker", args...)
	}
	return h.Command(ctx, append([]string{"docker"}, args...)...)
}

// Run runs a command and returns its combined output.
func (p Project) Run(ctx context.Context, args ...string) (string, error) {
	release, err := p.Host.Hold(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	out, err := p.Command(ctx, args...).CombinedOutput()
	return string(out), wrapOn(p.Host, err, out)
}

// PS returns all containers of the project, including stopped ones.
func (p Project) PS(ctx context.Context) ([]Container, error) {
	release, err := p.Host.Hold(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	cmd := p.Command(ctx, "ps", "-a", "--format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, wrapOn(p.Host, err, stderr.Bytes())
	}
	return ParsePS(out)
}

// Services lists the service names defined in the compose file.
func (p Project) Services(ctx context.Context) ([]string, error) {
	release, err := p.Host.Hold(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	cmd := p.Command(ctx, "config", "--services")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, wrapOn(p.Host, err, stderr.Bytes())
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

// wrapOn is wrap for a command on h, explaining ssh's own errors: ssh exits
// with 255 when it could not run the command at all, and is killed when the
// host does not answer in time.
func wrapOn(h *remote.Host, err error, out []byte) error {
	w := wrap(err, out)
	var ee *exec.ExitError
	if h != nil && w != nil && (errors.As(err, &ee) && (ee.ExitCode() == 255 || ee.ExitCode() == -1) || errors.Is(err, exec.ErrNotFound)) {
		return remote.Explain(h, w)
	}
	return w
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
// `docker ps` call and groups them by project working directory. The keys
// of a remote host are like ssh://host/srv/web, see DirKey.
func StateByDir(ctx context.Context, h *remote.Host) (map[string]*DirState, error) {
	out, err := dockerOutput(ctx, h, "ps", "-a",
		"--filter", "label=com.docker.compose.project", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParseDockerPS(out, h)
}

// DirKey is the key of a working directory on h: the cleaned path here, or
// ssh://host/path for a remote host, whose paths always use slashes.
func DirKey(h *remote.Host, dir string) string {
	if h == nil {
		return filepath.Clean(dir)
	}
	return h.Key(path.Clean(dir))
}

const (
	labelWorkingDir = "com.docker.compose.project.working_dir"
	labelService    = "com.docker.compose.service"
	labelOneoff     = "com.docker.compose.oneoff"
)

// ParseDockerPS parses `docker ps --format json` output from h.
func ParseDockerPS(data []byte, h *remote.Host) (map[string]*DirState, error) {
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
		dir = DirKey(h, dir)
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

// Stats is the resource use of one container.
type Stats struct {
	CPU string // e.g. "0.15%"
	Mem string // e.g. "52.3MiB"
}

// ContainerStats samples CPU and memory for the named containers. It takes
// about two seconds since docker measures CPU over an interval.
func ContainerStats(ctx context.Context, h *remote.Host, names []string) (map[string]Stats, error) {
	out, err := dockerOutput(ctx, h, append([]string{"stats", "--no-stream", "--format", "json"}, names...)...)
	if err != nil {
		return nil, err
	}
	return ParseStats(out)
}

// ParseStats parses `docker stats --format json`, keyed by container name.
func ParseStats(data []byte) (map[string]Stats, error) {
	res := map[string]Stats{}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	for dec.More() {
		var row struct {
			Name     string `json:"Name"`
			CPUPerc  string `json:"CPUPerc"`
			MemUsage string `json:"MemUsage"`
		}
		if err := dec.Decode(&row); err != nil {
			return nil, err
		}
		mem, _, _ := strings.Cut(row.MemUsage, " / ")
		res[row.Name] = Stats{CPU: row.CPUPerc, Mem: strings.TrimSpace(mem)}
	}
	return res, nil
}

// ServiceImage is the image a service's container was created from.
type ServiceImage struct {
	Dir, Service string
	Ref          string   // the image as written in the compose file
	ImageID      string   // the local image the container runs
	Digests      []string // registry digests of that image, like "sha256:..."
}

// ServiceImages lists the image of every compose-managed container on h,
// with the registry digests the image was pulled with. Locally built images
// have no digests. Dir is keyed like StateByDir.
func ServiceImages(ctx context.Context, h *remote.Host) ([]ServiceImage, error) {
	ids, err := dockerOutput(ctx, h, "ps", "-aq", "--filter", "label=com.docker.compose.project")
	if err != nil {
		return nil, err
	}
	containers := strings.Fields(string(ids))
	if len(containers) == 0 {
		return nil, nil
	}
	format := `{{.Image}}{{"\t"}}{{.Config.Image}}{{"\t"}}{{index .Config.Labels "` + labelWorkingDir +
		`"}}{{"\t"}}{{index .Config.Labels "` + labelService + `"}}`
	out, err := dockerOutput(ctx, h, append([]string{"inspect", "--format", format}, containers...)...)
	if err != nil {
		return nil, err
	}
	images, imageIDs := ParseContainerImages(out)
	for i := range images {
		images[i].Dir = DirKey(h, images[i].Dir)
	}
	if len(imageIDs) == 0 {
		return images, nil
	}
	// an image removed since the container was created makes docker exit
	// with an error, but it still prints the others
	digests, _ := dockerOutput(ctx, h, append([]string{"image", "inspect", "--format", `{{.Id}}{{"\t"}}{{json .RepoDigests}}`}, imageIDs...)...)
	byID := ParseRepoDigests(digests)
	for i := range images {
		images[i].Digests = byID[images[i].ImageID]
	}
	return images, nil
}

// ParseContainerImages parses lines of image id, image ref, working dir and
// service, and returns the images and the distinct image ids.
func ParseContainerImages(out []byte) ([]ServiceImage, []string) {
	var res []ServiceImage
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[2] == "" || f[3] == "" {
			continue
		}
		res = append(res, ServiceImage{Dir: f[2], Service: f[3], Ref: f[1], ImageID: f[0]})
		if !seen[f[0]] {
			seen[f[0]] = true
			ids = append(ids, f[0])
		}
	}
	return res, ids
}

// ParseRepoDigests parses lines of image id and its RepoDigests as JSON, and
// returns the digests without the repository, keyed by image id.
func ParseRepoDigests(out []byte) map[string][]string {
	res := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id, js, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		var repoDigests []string
		if json.Unmarshal([]byte(js), &repoDigests) != nil {
			continue
		}
		for _, rd := range repoDigests {
			if _, d, ok := strings.Cut(rd, "@"); ok {
				res[id] = append(res[id], d)
			}
		}
	}
	return res
}

func dockerOutput(ctx context.Context, h *remote.Host, args ...string) ([]byte, error) {
	release, err := h.Hold(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	cmd := Docker(ctx, h, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, wrapOn(h, err, stderr.Bytes())
	}
	return out, nil
}

// OldImage is an untagged image no container uses: usually the previous
// version of an image, left behind by a pull.
type OldImage struct {
	ID      string
	Repo    string // the repository it was pulled from, "" if unknown
	Size    int64
	Created time.Time
}

// OldImages lists the untagged images that no container, running or not,
// uses.
func OldImages(ctx context.Context) ([]OldImage, error) {
	out, err := dockerOutput(ctx, nil, "image", "ls", "-q", "--no-trunc", "--filter", "dangling=true")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil, nil
	}
	used := map[string]bool{}
	out, err = dockerOutput(ctx, nil, "ps", "-aq", "--no-trunc")
	if err != nil {
		return nil, err
	}
	if containers := strings.Fields(string(out)); len(containers) > 0 {
		// a container removed meanwhile makes docker exit with an error,
		// but it still prints the others
		out, _ = dockerOutput(ctx, nil, append([]string{"inspect", "--format", "{{.Image}}"}, containers...)...)
		for _, id := range strings.Fields(string(out)) {
			used[id] = true
		}
	}
	format := `{{.Id}}{{"\t"}}{{json .RepoDigests}}{{"\t"}}{{.Size}}{{"\t"}}{{.Created}}`
	out, _ = dockerOutput(ctx, nil, append([]string{"image", "inspect", "--format", format}, ids...)...)
	return ParseOldImages(out, used), nil
}

// ParseOldImages parses lines of image id, RepoDigests as JSON, size and
// creation time, leaving out the images in used.
func ParseOldImages(out []byte, used map[string]bool) []OldImage {
	var res []OldImage
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || used[f[0]] || !strings.HasPrefix(f[0], "sha256:") {
			continue
		}
		img := OldImage{ID: f[0]}
		var digests []string
		if json.Unmarshal([]byte(f[1]), &digests) == nil && len(digests) > 0 {
			img.Repo, _, _ = strings.Cut(digests[0], "@")
		}
		img.Size, _ = strconv.ParseInt(f[2], 10, 64)
		img.Created, _ = time.Parse(time.RFC3339Nano, f[3])
		res = append(res, img)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Repo != res[j].Repo {
			return res[i].Repo < res[j].Repo
		}
		return res[i].Created.After(res[j].Created)
	})
	return res
}
