package ui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/remote"
)

// Editing a compose or env file: the editor gets the file and lazycompose
// watches it, so every save is validated and offered to apply. A terminal
// editor like vim keeps the screen until it quits, which ends the edit. A
// graphical one, like code, hands the file to its window and returns at
// once; the file is then watched until another edit starts or lazycompose
// quits, so the window can stay open.
//
// A file of a remote environment is copied into a folder only the user can
// read, the editor works on the copy, and each save is written back to the
// host, unless the file changed there meanwhile.

const (
	// watchEvery is how often a watched file is checked for saves.
	watchEvery = time.Second
	// quickEditor: an editor that returns sooner handed the file to a
	// window that keeps running.
	quickEditor = 2 * time.Second
	// maxEditFile limits the remote files that are copied for editing.
	maxEditFile = 1 << 20
	// editDirPrefix starts the folders that hold copies of remote files;
	// the process id follows, so a later start can tell leftovers.
	editDirPrefix = "lazycompose-edit-"
)

type editWatch struct {
	id   int
	env  discover.Env
	file string   // the file the editor has open
	hash [32]byte // of file when it was last handled

	// a remote edit
	remote       string // the file on env.Host
	dir          string // the private folder that holds the copy
	server       [32]byte
	serverExists bool // whether the file was on the host when last read
	uploading    bool
	again        bool // saved again while uploading

	watching bool // the editor returned quickly, its window has the file
	closing  bool // the editor quit; end once the last save is handled
}

func (w *editWatch) name() string {
	if w.remote != "" {
		return path.Base(w.remote)
	}
	return filepath.Base(w.file)
}

type (
	// editorMsg comes when the editor exits.
	editorMsg struct {
		id   int
		took time.Duration
		err  error
	}
	editTickMsg  struct{ id int }
	editReadyMsg struct { // a remote file was copied
		id     int
		dir    string
		file   string
		server [32]byte
		exists bool
		err    error
	}
	uploadedMsg struct {
		id     int
		server [32]byte
		err    error
	}
)

// errChanged: the remote file changed since it was copied.
var errChanged = errors.New("changed on the server since it was opened")

// startEdit opens file of e in the editor and watches it. It ends the edit
// before, if there is one.
func (m *Model) startEdit(e discover.Env, file string) tea.Cmd {
	m.endEdit()
	m.editID++
	w := &editWatch{id: m.editID, env: e}
	m.edit = w
	if !e.Remote() {
		w.file = file
		w.hash = hashFile(file)
		return m.runEditor(w)
	}
	w.remote = file
	return tea.Batch(m.startBusy("opening "+path.Base(file)+" from "+e.Host.Label()), copyForEdit(w.id, e, file))
}

func (m *Model) runEditor(w *editWatch) tea.Cmd {
	cmd, err := editorCommand(m.opts.Editor, w.file)
	if err != nil {
		m.endEdit()
		m.setStatus("editor: "+err.Error(), true)
		return nil
	}
	id, start := w.id, time.Now()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorMsg{id: id, took: time.Since(start), err: err}
	})
}

// endEdit stops watching, and removes the copy of a remote file.
func (m *Model) endEdit() {
	if w := m.edit; w != nil && w.dir != "" {
		_ = os.RemoveAll(w.dir)
	}
	m.edit = nil
}

// current returns the edit a message with id is about, if it still runs.
func (m *Model) currentEdit(id int) *editWatch {
	if m.edit != nil && m.edit.id == id {
		return m.edit
	}
	return nil
}

func (m *Model) onEditor(msg editorMsg) tea.Cmd {
	w := m.currentEdit(msg.id)
	if w == nil {
		return nil
	}
	m.composeTarget = logTarget{} // show the file as it is now
	if msg.err != nil {
		m.endEdit()
		m.setStatus("editor: "+msg.err.Error(), true)
		return nil
	}
	cmd := m.checkSaved(w)
	if msg.took < quickEditor {
		w.watching = true
		if cmd == nil {
			m.setStatus("editing "+w.name()+": every save is checked and offered to apply", false)
		}
		return tea.Batch(cmd, editTick(w.id))
	}
	if cmd == nil {
		m.endEdit()
		m.setStatus("no changes", false)
		return nil
	}
	w.closing = true
	if !w.uploading {
		m.endEdit()
	}
	return cmd
}

func editTick(id int) tea.Cmd {
	return tea.Tick(watchEvery, func(time.Time) tea.Msg { return editTickMsg{id: id} })
}

func (m *Model) onEditTick(msg editTickMsg) tea.Cmd {
	w := m.currentEdit(msg.id)
	if w == nil || !w.watching {
		return nil
	}
	return tea.Batch(m.checkSaved(w), editTick(w.id))
}

// checkSaved handles a save of the watched file: a local file is
// validated, a remote one written back first. It returns nil when the file
// did not change.
func (m *Model) checkSaved(w *editWatch) tea.Cmd {
	h := hashFile(w.file)
	if h == w.hash {
		return nil
	}
	if w.remote == "" {
		w.hash = h
		return tea.Batch(m.startBusy("validating "+w.env.Rel), validate(w.env))
	}
	if w.uploading {
		w.again = true
		return nil
	}
	data, err := os.ReadFile(w.file)
	if err != nil {
		m.setStatus("reading "+w.name()+": "+err.Error(), true)
		return nil
	}
	w.hash = h
	w.uploading = true
	return tea.Batch(m.startBusy("saving "+w.name()+" to "+w.env.Host.Label()),
		upload(w.id, w.env, w.remote, data, w.server, w.serverExists))
}

// copyForEdit copies a remote file into a new private folder.
func copyForEdit(id int, e discover.Env, file string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, exists, err := e.Host.ReadFile(ctx, file, maxEditFile)
		if err != nil {
			return editReadyMsg{id: id, err: err}
		}
		dir, err := os.MkdirTemp("", fmt.Sprintf("%s%d-*", editDirPrefix, os.Getpid()))
		if err != nil {
			return editReadyMsg{id: id, err: err}
		}
		local := filepath.Join(dir, path.Base(file))
		if err := os.WriteFile(local, data, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return editReadyMsg{id: id, err: err}
		}
		msg := editReadyMsg{id: id, dir: dir, file: local, exists: exists}
		if exists {
			msg.server = sha256.Sum256(data)
		}
		return msg
	}
}

func (m *Model) onEditReady(msg editReadyMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	w := m.currentEdit(msg.id)
	if w == nil {
		if msg.dir != "" {
			_ = os.RemoveAll(msg.dir)
		}
		return nil
	}
	if msg.err != nil {
		m.endEdit()
		m.setStatus("opening the file: "+firstLine(msg.err.Error()), true)
		return nil
	}
	w.dir, w.file = msg.dir, msg.file
	w.server, w.serverExists = msg.server, msg.exists
	w.hash = hashFile(w.file)
	m.setStatus("", false)
	return m.runEditor(w)
}

// upload writes data back to the host, if the file there is still the one
// that was copied (server, or none when !exists).
func upload(id int, e discover.Env, file string, data []byte, server [32]byte, exists bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		now, there, err := e.Host.ReadFile(ctx, file, maxEditFile)
		switch {
		case errors.Is(err, remote.ErrTooBig):
			return uploadedMsg{id: id, err: errChanged}
		case err != nil:
			return uploadedMsg{id: id, err: err}
		case there != exists || there && sha256.Sum256(now) != server:
			return uploadedMsg{id: id, err: errChanged}
		}
		mode := ""
		if !exists {
			mode = "600" // it can hold secrets
		}
		if err := e.Host.WriteFile(ctx, file, data, mode); err != nil {
			return uploadedMsg{id: id, err: err}
		}
		return uploadedMsg{id: id, server: sha256.Sum256(data)}
	}
}

func (m *Model) onUploaded(msg uploadedMsg) tea.Cmd {
	m.busy = max(0, m.busy-1)
	w := m.currentEdit(msg.id)
	if w == nil {
		return nil
	}
	w.uploading = false
	if errors.Is(msg.err, errChanged) {
		// keep the copy until quitting, so the work is not lost
		m.setStatus(fmt.Sprintf("not saved: %s changed on %s meanwhile; yours is in %s until you quit", w.name(), w.env.Host.Label(), w.file), true)
		m.leftovers = append(m.leftovers, w.dir)
		w.dir = ""
		m.endEdit()
		return nil
	}
	if msg.err != nil {
		m.setStatus("saving "+w.name()+": "+firstLine(msg.err.Error())+"; trying again", true)
		w.hash = [32]byte{} // the next check tries again
		if w.closing {
			w.watching = true // keep the copy until it is saved
			return editTick(w.id)
		}
		return nil
	}
	w.server, w.serverExists = msg.server, true
	cmds := []tea.Cmd{m.startBusy("validating " + w.env.Rel), validate(w.env)}
	if w.again {
		w.again = false
		cmds = append(cmds, m.checkSaved(w))
	}
	if w.closing && !w.uploading {
		m.endEdit()
	}
	return tea.Batch(cmds...)
}
