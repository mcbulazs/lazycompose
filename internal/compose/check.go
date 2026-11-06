package compose

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// run, lookPath and checkTimeout are swapped out in tests.
var (
	// checkTimeout bounds each command of Check, so an unreachable
	// DOCKER_HOST does not hang the start.
	checkTimeout = 10 * time.Second

	lookPath = exec.LookPath
	run      = func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
)

// Check makes sure docker, the compose plugin and the daemon can be used,
// and says what to do when one of them cannot.
func Check(ctx context.Context) error {
	if _, err := lookPath("docker"); err != nil {
		return errors.New("docker was not found in PATH; install Docker first: https://docs.docker.com/engine/install/")
	}
	if out, err := runCheck(ctx, "compose", "version"); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("docker compose version did not answer in time")
		}
		return withDetail("docker compose is not installed; install the compose plugin (docker-compose-plugin): https://docs.docker.com/compose/install/", out)
	}
	if out, err := runCheck(ctx, "version", "--format", "{{.Server.Version}}"); err != nil {
		return daemonError(out, err)
	}
	return nil
}

func runCheck(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	out, err := run(ctx, args...)
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// daemonError explains why `docker version` could not reach the daemon.
func daemonError(out []byte, err error) error {
	low := strings.ToLower(firstLine(out))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("the Docker daemon did not answer in time; check that it is running and DOCKER_HOST is right")
	case strings.Contains(low, "permission denied"):
		return withDetail("no permission to use the Docker daemon; add your user to the docker group (sudo usermod -aG docker $USER, then log in again) or run lazycompose with sudo", out)
	case strings.Contains(low, "connect"):
		// "Cannot connect to the Docker daemon", "failed to connect to the
		// docker API", "error during connect"
		return withDetail("cannot reach the Docker daemon; check that it is running (sudo systemctl start docker, or open Docker Desktop)", out)
	case low != "":
		return withDetail("cannot use the Docker daemon", out)
	}
	return fmt.Errorf("cannot use the Docker daemon: %w", err)
}

// withDetail adds the first line of docker's output to msg, if there is one.
func withDetail(msg string, out []byte) error {
	if l := firstLine(out); l != "" {
		return fmt.Errorf("%s\ndocker: %s", msg, strings.TrimPrefix(l, "docker: "))
	}
	return errors.New(msg)
}

// firstLine is the first line of out that is not empty, without control
// characters, so it is safe to print.
func firstLine(out []byte) string {
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, l)
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
