package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeDocker replaces docker for Check: found says whether it is in PATH,
// and answers maps the first argument to docker's output and error.
func fakeDocker(t *testing.T, found bool, answers map[string]func(context.Context) ([]byte, error)) {
	t.Helper()
	oldLook, oldRun := lookPath, run
	t.Cleanup(func() { lookPath, run = oldLook, oldRun })
	lookPath = func(string) (string, error) {
		if !found {
			return "", errors.New("not found")
		}
		return "/usr/bin/docker", nil
	}
	run = func(ctx context.Context, args ...string) ([]byte, error) {
		if a, ok := answers[args[0]]; ok {
			return a(ctx)
		}
		return nil, nil
	}
}

func fail(out string) func(context.Context) ([]byte, error) {
	return func(context.Context) ([]byte, error) { return []byte(out), errors.New("exit status 1") }
}

func TestCheck(t *testing.T) {
	hang := func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, errors.New("signal: killed")
	}
	tests := []struct {
		name    string
		found   bool
		answers map[string]func(context.Context) ([]byte, error)
		want    []string // parts of the error, none for no error
	}{
		{"all good", true, nil, nil},
		{"no docker", false, nil, []string{"docker was not found in PATH"}},
		{"no compose", true, map[string]func(context.Context) ([]byte, error){
			"compose": fail("docker: 'compose' is not a docker command.\nSee 'docker --help'\n"),
		}, []string{"compose plugin", "\ndocker: 'compose' is not a docker command."}},
		{"no permission", true, map[string]func(context.Context) ([]byte, error){
			"version": fail("\npermission denied while trying to connect to the docker API at unix:///var/run/docker.sock\n"),
		}, []string{"docker group", "sudo", "docker: permission denied while"}},
		{"not running", true, map[string]func(context.Context) ([]byte, error){
			"version": fail("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n"),
		}, []string{"cannot reach the Docker daemon", "systemctl start docker"}},
		{"not running, docker 29", true, map[string]func(context.Context) ([]byte, error){
			"version": fail("failed to connect to the docker API at unix:///var/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /var/run/docker.sock: connect: no such file or directory\n"),
		}, []string{"cannot reach the Docker daemon", "docker: failed to connect"}},
		{"other error", true, map[string]func(context.Context) ([]byte, error){
			"version": fail("Error response from daemon: \x1b[31mclient version 1.52 is too new\x1b[0m\n"),
		}, []string{"cannot use the Docker daemon\ndocker: Error response from daemon: [31mclient version 1.52 is too new[0m"}},
		{"no output", true, map[string]func(context.Context) ([]byte, error){
			"version": fail(""),
		}, []string{"cannot use the Docker daemon: exit status 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeDocker(t, tt.found, tt.answers)
			err := Check(context.Background())
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("Check() = %v, want no error", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Check() = nil, want an error with %q", tt.want)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("Check() = %q, want it to contain %q", err, w)
				}
			}
			if strings.ContainsRune(err.Error(), '\x1b') {
				t.Errorf("Check() = %q, has an escape character", err)
			}
		})
	}

	t.Run("daemon hangs", func(t *testing.T) {
		fakeDocker(t, true, map[string]func(context.Context) ([]byte, error){"version": hang})
		old := checkTimeout
		checkTimeout = 10 * time.Millisecond
		t.Cleanup(func() { checkTimeout = old })
		err := Check(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Docker daemon did not answer in time") {
			t.Fatalf("Check() = %v, want a timeout", err)
		}
	})
}
