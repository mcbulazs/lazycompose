package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// The rule for the defaults: a lowercase key acts on the selection, its
// capital is the stronger version (r restart, R force recreate).

type keyMap struct {
	MoveUp, MoveDown, NextPane, PrevPane     key.Binding
	FocusEnvs, FocusServices, FocusLogs      key.Binding
	Filter, Problems                         key.Binding
	Edit, EditEnv                            key.Binding
	NewEnv, MoveEnv, Pin                     key.Binding
	Start, Stop, Restart, Recreate, Toggle   key.Binding
	Pull, UpdateAll                          key.Binding
	Logs, Shell, Folder, Refresh, Help, Quit key.Binding
	QuitToFolder, Commands                   key.Binding
	CleanImages, Zoom, ComposeView           key.Binding
}

// action is a key binding as it is named in the config file.
type action struct {
	name string
	keys []string
	help string
	get  func(*keyMap) *key.Binding
}

var actions = []action{
	{"move_up", []string{"k", "up"}, "up", func(k *keyMap) *key.Binding { return &k.MoveUp }},
	{"move_down", []string{"j", "down"}, "down", func(k *keyMap) *key.Binding { return &k.MoveDown }},
	{"next_pane", []string{"tab"}, "next pane", func(k *keyMap) *key.Binding { return &k.NextPane }},
	{"prev_pane", []string{"shift+tab"}, "previous pane", func(k *keyMap) *key.Binding { return &k.PrevPane }},
	{"focus_environments", []string{"1"}, "environments", func(k *keyMap) *key.Binding { return &k.FocusEnvs }},
	{"focus_services", []string{"2"}, "services", func(k *keyMap) *key.Binding { return &k.FocusServices }},
	{"focus_logs", []string{"3"}, "logs", func(k *keyMap) *key.Binding { return &k.FocusLogs }},
	{"filter", []string{"/"}, "filter / search", func(k *keyMap) *key.Binding { return &k.Filter }},
	{"problems", []string{"!"}, "problems only", func(k *keyMap) *key.Binding { return &k.Problems }},
	{"edit", []string{"e"}, "edit", func(k *keyMap) *key.Binding { return &k.Edit }},
	{"edit_env", []string{"E"}, "edit env file", func(k *keyMap) *key.Binding { return &k.EditEnv }},
	{"new_env", []string{"n"}, "new environment", func(k *keyMap) *key.Binding { return &k.NewEnv }},
	{"move_env", []string{"m"}, "move/rename", func(k *keyMap) *key.Binding { return &k.MoveEnv }},
	{"pin", []string{"f"}, "pin/unpin", func(k *keyMap) *key.Binding { return &k.Pin }},
	{"up", []string{"u"}, "up -d", func(k *keyMap) *key.Binding { return &k.Start }},
	{"down", []string{"d"}, "down", func(k *keyMap) *key.Binding { return &k.Stop }},
	{"restart", []string{"r"}, "restart", func(k *keyMap) *key.Binding { return &k.Restart }},
	{"recreate", []string{"R"}, "force recreate", func(k *keyMap) *key.Binding { return &k.Recreate }},
	{"stop_start", []string{"s"}, "stop/start service", func(k *keyMap) *key.Binding { return &k.Toggle }},
	{"pull", []string{"p"}, "pull", func(k *keyMap) *key.Binding { return &k.Pull }},
	{"update_all", []string{"U"}, "update all", func(k *keyMap) *key.Binding { return &k.UpdateAll }},
	{"clean_images", []string{"P"}, "remove old images", func(k *keyMap) *key.Binding { return &k.CleanImages }},
	{"logs", []string{"l"}, "fetch logs", func(k *keyMap) *key.Binding { return &k.Logs }},
	{"shell", []string{"x"}, "shell", func(k *keyMap) *key.Binding { return &k.Shell }},
	{"folder_shell", []string{"X"}, "shell in folder", func(k *keyMap) *key.Binding { return &k.Folder }},
	{"zoom", []string{"z"}, "zoom pane", func(k *keyMap) *key.Binding { return &k.Zoom }},
	{"compose_view", []string{"v"}, "logs / compose file", func(k *keyMap) *key.Binding { return &k.ComposeView }},
	{"refresh", []string{"ctrl+r"}, "refresh", func(k *keyMap) *key.Binding { return &k.Refresh }},
	{"commands", []string{":"}, "run", func(k *keyMap) *key.Binding { return &k.Commands }},
	{"help", []string{"?"}, "keys", func(k *keyMap) *key.Binding { return &k.Help }},
	{"quit", []string{"q"}, "quit", func(k *keyMap) *key.Binding { return &k.Quit }},
	{"quit_to_folder", []string{"Q"}, "quit to folder", func(k *keyMap) *key.Binding { return &k.QuitToFolder }},
}

// fixedKeys keep their meaning wherever they apply, so they cannot be bound.
var fixedKeys = map[string]string{
	"ctrl+c": "always quits",
	"esc":    "cancels and clears",
	"enter":  "confirms input",
}

// ActionNames lists the actions that can be set under keys in the config.
func ActionNames() []string {
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = a.name
	}
	return names
}

// DefaultKeys returns the default keys of an action.
func DefaultKeys(name string) []string {
	for _, a := range actions {
		if a.name == name {
			return a.keys
		}
	}
	return nil
}

var keys = mustKeys(nil)

func mustKeys(overrides map[string][]string) keyMap {
	k, err := buildKeys(overrides)
	if err != nil {
		panic(err)
	}
	return k
}

// SetKeys applies key overrides from the config on top of the defaults.
func SetKeys(overrides map[string][]string) error {
	k, err := buildKeys(overrides)
	if err != nil {
		return err
	}
	keys = k
	return nil
}

func buildKeys(overrides map[string][]string) (keyMap, error) {
	var km keyMap
	byName := map[string]action{}
	for _, a := range actions {
		byName[a.name] = a
		*a.get(&km) = key.NewBinding(key.WithKeys(a.keys...), key.WithHelp(keyLabel(a.keys), a.help))
	}

	for _, name := range sortedKeys(overrides) {
		a, ok := byName[name]
		if !ok {
			return km, fmt.Errorf("keys.%s: unknown action, use one of: %s", name, strings.Join(ActionNames(), ", "))
		}
		ks := overrides[name]
		b := a.get(&km)
		if len(ks) == 0 {
			b.SetEnabled(false) // an empty list unbinds the action
			continue
		}
		for _, k := range ks {
			if why, fixed := fixedKeys[k]; fixed {
				return km, fmt.Errorf("keys.%s: %q cannot be bound, it %s", name, k, why)
			}
		}
		b.SetKeys(ks...)
		b.SetHelp(keyLabel(ks), a.help)
	}

	// one key, one action
	owner := map[string]string{}
	var clashes []string
	for _, a := range actions {
		b := a.get(&km)
		if !b.Enabled() {
			continue
		}
		for _, k := range b.Keys() {
			if other, taken := owner[k]; taken {
				clashes = append(clashes, fmt.Sprintf("%q is bound to both %s and %s", k, other, a.name))
				continue
			}
			owner[k] = a.name
		}
	}
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return km, fmt.Errorf("keys: %s", strings.Join(clashes, "; "))
	}
	return km, nil
}

var arrows = strings.NewReplacer("up", "↑", "down", "↓", "left", "←", "right", "→")

// keyLabel is how keys are shown in the help, with arrows for arrow keys.
func keyLabel(ks []string) string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k
		if k == "up" || k == "down" || k == "left" || k == "right" {
			out[i] = arrows.Replace(k)
		}
	}
	return strings.Join(out, "/")
}
