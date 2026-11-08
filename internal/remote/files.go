package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// ErrTooBig is returned by ReadFile for a file over the limit.
var ErrTooBig = errors.New("the file is too big")

// ReadFile reads the file p on the host, with head so that no more than
// limit bytes come over. A file that does not exist is not an error: exists
// is false. A bigger file gives its first limit bytes and ErrTooBig.
func (h *Host) ReadFile(ctx context.Context, p string, limit int) (data []byte, exists bool, err error) {
	release, err := h.Hold(ctx)
	if err != nil {
		return nil, false, err
	}
	defer release()
	cmd := h.Command(ctx, "head", "-c", strconv.Itoa(limit+1), "--", p)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "No such file") {
			return nil, false, nil
		}
		return nil, false, h.fail(err, stderr.String())
	}
	if len(out) > limit {
		return out[:limit], true, fmt.Errorf("%s: %w, over %d bytes", p, ErrTooBig, limit)
	}
	return out, true, nil
}

// WriteFile replaces what p on the host holds with data. It writes into the
// file that is there, with tee, so its owner and permissions stay. With a
// mode like "600", the file does not exist yet and is made first with that
// mode; files edited here, like .env, can hold secrets, so 600 is the one to
// use unless the file is meant to be read by others.
func (h *Host) WriteFile(ctx context.Context, p string, data []byte, mode string) error {
	if mode != "" {
		if err := h.run(ctx, nil, "touch", "--", p); err != nil {
			return err
		}
		// "--" before the mode: macOS' chmod takes the first operand as
		// the mode, so a "--" after it would be read as a file
		if err := h.run(ctx, nil, "chmod", "--", mode, p); err != nil {
			return err
		}
	}
	return h.run(ctx, bytes.NewReader(data), "tee", "--", p)
}

// Run runs args on the host, for commands whose output is not needed.
func (h *Host) Run(ctx context.Context, args ...string) error { return h.run(ctx, nil, args...) }

func (h *Host) run(ctx context.Context, stdin io.Reader, args ...string) error {
	release, err := h.Hold(ctx)
	if err != nil {
		return err
	}
	defer release()
	cmd := h.Command(ctx, args...)
	cmd.Stdin = stdin
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return h.fail(err, stderr.String())
	}
	return nil
}

// Exists reports whether p is on the host, as a file, folder or link.
func (h *Host) Exists(ctx context.Context, p string) (bool, error) {
	release, err := h.Hold(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	cmd := h.Command(ctx, "ls", "-d", "--", p)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	switch {
	case err == nil:
		return true, nil
	case strings.Contains(stderr.String(), "No such file"):
		return false, nil
	}
	return false, h.fail(err, stderr.String())
}

// RealPath resolves the links in p, which must exist, with readlink -f.
func (h *Host) RealPath(ctx context.Context, p string) (string, error) {
	release, err := h.Hold(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	cmd := h.Command(ctx, "readlink", "-f", "--", p)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", h.fail(err, stderr.String())
	}
	real := strings.TrimSpace(string(out))
	if !strings.HasPrefix(real, "/") {
		return "", fmt.Errorf("%s: readlink -f gave %q for %s", h.Label(), firstLine(real), p)
	}
	return real, nil
}

// fail is the error of a command on the host: what the command wrote to
// stderr, or ssh's own error explained. ssh exits with 255 when it could
// not run the command, and is killed when the host does not answer.
func (h *Host) fail(err error, stderr string) error {
	msg := firstLine(stderr)
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return Explain(h, err)
	}
	switch code := ee.ExitCode(); {
	case code == 255 && msg != "":
		return Explain(h, errors.New(stderr))
	case code == 255 || code == -1:
		return Explain(h, err)
	case msg != "":
		return fmt.Errorf("%s: %s", h.Label(), msg)
	}
	return Explain(h, err)
}

// firstLine is the first line of s that is not empty, without control
// characters, so it is safe to print.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
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
