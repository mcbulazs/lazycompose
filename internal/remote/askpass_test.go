package remote

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// askpass runs RunAskpass as ssh would, with env from a, and returns its
// exit code and what it printed.
func askpass(t *testing.T, env []string, prompt string) (int, string) {
	t.Helper()
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == askpassSocketEnv || k == askpassTokenEnv {
			t.Setenv(k, v)
		}
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	code, ok := RunAskpass([]string{"lazycompose", prompt})
	os.Stdout = stdout
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if !ok {
		t.Fatal("RunAskpass should see the askpass environment")
	}
	return code, string(out)
}

func TestAskpass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("logins on Windows use ssh-agent")
	}
	if _, ok := RunAskpass([]string{"lazycompose"}); ok {
		t.Fatal("a normal start is not askpass")
	}
	a, err := NewAsker()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if fi, err := os.Stat(a.dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the socket's folder should be private: %v %v", fi.Mode(), err)
	}
	env, err := a.env()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=force", askpassSocketEnv + "=", askpassTokenEnv + "="} {
		if !strings.Contains(joined, want) {
			t.Errorf("env is missing %s", want)
		}
	}

	answer := func(do func(Question)) chan Question {
		got := make(chan Question, 1)
		go func() {
			select {
			case q := <-a.Questions:
				got <- q
				do(q)
			case <-time.After(5 * time.Second):
				close(got)
			}
		}()
		return got
	}

	// a passphrase, with its spaces kept exactly
	got := answer(func(q Question) { q.Answer("  my secret  pass ") })
	code, out := askpass(t, env, "Enter passphrase for key '/home/me/.ssh/id_ed25519':\x1b[31m ")
	q := <-got
	if code != 0 || out != "  my secret  pass \n" {
		t.Errorf("code %d, printed %q", code, out)
	}
	if !q.Secret || q.Prompt != "Enter passphrase for key '/home/me/.ssh/id_ed25519':[31m" {
		t.Errorf("question %+v", q)
	}

	// trusting a host key is asked in the open
	got = answer(func(q Question) { q.Answer("yes") })
	if code, out := askpass(t, env, "Are you sure you want to continue connecting (yes/no/[fingerprint])?"); code != 0 || out != "yes\n" {
		t.Errorf("code %d, printed %q", code, out)
	}
	if q := <-got; q.Secret {
		t.Error("a yes/no question should not be hidden")
	}

	// cancelling fails the question
	answer(func(q Question) { q.Cancel() })
	if code, out := askpass(t, env, "password:"); code != 1 || out != "" {
		t.Errorf("cancelled: code %d, printed %q", code, out)
	}

	// a wrong token is not asked at all
	bad := append(env, askpassTokenEnv+"=wrong")
	got = answer(func(q Question) { q.Answer("leak") })
	if code, out := askpass(t, bad, "password:"); code != 1 || out != "" {
		t.Errorf("wrong token: code %d, printed %q", code, out)
	}
	if _, ok := <-got; ok {
		t.Error("a wrong token should not reach the questions")
	}

	a.Close()
	if _, err := os.Stat(a.dir); !os.IsNotExist(err) {
		t.Errorf("Close should remove the folder: %v", err)
	}
}
