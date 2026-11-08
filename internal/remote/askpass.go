package remote

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ssh asks for a key's passphrase, a password or whether to trust a host
// key on the terminal, which belongs to lazycompose's screen. So the login
// runs ssh with SSH_ASKPASS set to lazycompose itself: ssh starts it with
// the question, RunAskpass hands the question over a private socket to the
// lazycompose that started ssh, which asks in a popup, and the answer goes
// back the same way to ssh. The answer is never written anywhere.

const (
	askpassSocketEnv = "LAZYCOMPOSE_ASKPASS_SOCKET"
	askpassTokenEnv  = "LAZYCOMPOSE_ASKPASS_TOKEN"
	// AskpassDirPrefix starts the folder of the socket; the process id
	// follows, so leftovers of a crash can be told apart.
	AskpassDirPrefix = "lazycompose-askpass-"
	// askpassWait is how long a question may wait for its answer.
	askpassWait = 10 * time.Minute
)

// RunAskpass is lazycompose run by ssh as SSH_ASKPASS. It reports false
// when lazycompose was started normally. Otherwise it passes args[1], the
// question, to the lazycompose that started ssh, prints the answer for ssh
// and returns the exit code: 0 with an answer, 1 when the question was
// cancelled or could not be asked.
func RunAskpass(args []string) (code int, isAskpass bool) {
	sock := os.Getenv(askpassSocketEnv)
	if sock == "" {
		return 0, false
	}
	prompt := ""
	if len(args) > 1 {
		prompt = args[1]
	}
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose askpass:", err)
		return 1, true
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(askpassWait))
	if _, err := fmt.Fprintf(conn, "%s\n%s\n", os.Getenv(askpassTokenEnv), oneLine(prompt)); err != nil {
		return 1, true
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	answer, ok := strings.CutPrefix(reply, "ok ")
	if err != nil || !ok {
		return 1, true
	}
	if _, err := os.Stdout.WriteString(answer); err != nil {
		return 1, true
	}
	return 0, true
}

// oneLine joins the lines of a question, so it fits the protocol's line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// Question is something ssh asks during a login.
type Question struct {
	Prompt string // ssh's question, without control characters
	// Secret is set for a passphrase or password: what is typed is hidden.
	// Questions like trusting a host key take a visible yes or no.
	Secret bool
	reply  chan string // "ok answer" or "cancel"
}

// Answer sends the answer to ssh, exactly as typed; only line breaks,
// which would end it early, are left out.
func (q Question) Answer(s string) {
	q.reply <- "ok " + strings.NewReplacer("\r", "", "\n", "").Replace(s) + "\n"
}

// Cancel tells ssh there is no answer, which ends the login.
func (q Question) Cancel() { q.reply <- "cancel" }

// Asker receives the questions of ssh logins on a socket in a folder only
// this user can open, and hands them out on Questions.
type Asker struct {
	Questions chan Question
	dir       string
	sock      string
	token     string
	ln        net.Listener
}

// NewAsker starts listening for questions.
func NewAsker() (*Asker, error) {
	dir, err := os.MkdirTemp("", fmt.Sprintf("%s%d-*", AskpassDirPrefix, os.Getpid()))
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	a := &Asker{Questions: make(chan Question), dir: dir, sock: sock, token: hex.EncodeToString(b), ln: ln}
	go a.serve()
	return a, nil
}

func (a *Asker) serve() {
	for {
		conn, err := a.ln.Accept()
		if err != nil {
			return // closed
		}
		go a.handle(conn)
	}
}

func (a *Asker) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(askpassWait))
	r := bufio.NewReader(conn)
	token, err1 := r.ReadString('\n')
	prompt, err2 := r.ReadString('\n')
	if err1 != nil || err2 != nil || subtle.ConstantTimeCompare([]byte(strings.TrimSuffix(token, "\n")), []byte(a.token)) != 1 {
		return
	}
	prompt = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			return -1
		}
		return r
	}, strings.TrimSpace(prompt))
	low := strings.ToLower(prompt)
	q := Question{Prompt: prompt, Secret: !strings.Contains(low, "yes/no"), reply: make(chan string, 1)}
	select {
	case a.Questions <- q:
	case <-time.After(askpassWait):
		return
	}
	r2 := <-q.reply
	if !strings.HasSuffix(r2, "\n") {
		r2 += "\n"
	}
	_, _ = conn.Write([]byte(r2))
}

// Close stops listening and removes the socket's folder.
func (a *Asker) Close() {
	_ = a.ln.Close()
	_ = os.RemoveAll(a.dir)
}

// env is what ssh needs to ask through lazycompose: itself as the askpass
// program, used even with a terminal (SSH_ASKPASS_REQUIRE, OpenSSH 8.4),
// and DISPLAY, which older versions want before they use it.
func (a *Asker) env() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(),
		"SSH_ASKPASS="+exe,
		"SSH_ASKPASS_REQUIRE=force",
		askpassSocketEnv+"="+a.sock,
		askpassTokenEnv+"="+a.token)
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=lazycompose")
	}
	return env, nil
}

// AskingLoginCommand is LoginCommand with ssh's questions going to a, and
// without the terminal, so ssh cannot ask on it. Cancelling ctx ends the
// login.
func (h *Host) AskingLoginCommand(ctx context.Context, a *Asker) (*exec.Cmd, error) {
	env, err := a.env()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, sshProgram, h.sshArgs(sshLogin, []string{"true"})...)
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	detach(cmd)
	return cmd, nil
}

// NewQuestion makes a question without ssh, for tests: the returned
// function waits for the reply as ssh would get it.
func NewQuestion(prompt string, secret bool) (Question, func() string) {
	q := Question{Prompt: prompt, Secret: secret, reply: make(chan string, 1)}
	return q, func() string { return <-q.reply }
}
