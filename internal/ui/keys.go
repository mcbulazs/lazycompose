package ui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up, Down, Tab, Filter      key.Binding
	Edit, Start, Stop, Restart key.Binding
	Pull, Logs, Shell, Refresh key.Binding
	UpdateAll, Problems        key.Binding
	Folder, Toggle             key.Binding
	Help, Quit                 key.Binding
}

var keys = keyMap{
	Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
	Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
	Tab:       key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch pane")),
	Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Start:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up -d")),
	Stop:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "down")),
	Restart:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
	Pull:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
	Logs:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	Shell:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "shell")),
	UpdateAll: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update all")),
	Problems:  key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "problems only")),
	Folder:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "shell in folder")),
	Toggle:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stop/start service")),
	Refresh:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Filter, k.Edit, k.Start, k.Stop, k.Restart, k.Pull, k.Logs, k.Shell, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Tab, k.Filter, k.Problems},
		{k.Edit, k.Start, k.Stop, k.Restart},
		{k.Toggle, k.Pull, k.UpdateAll},
		{k.Logs, k.Shell, k.Folder},
		{k.Refresh, k.Help, k.Quit},
	}
}
