package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
	"github.com/mcbulazs/lazycompose/internal/ui"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Roots    []string                     `yaml:"roots"`
	MaxDepth int                          `yaml:"max_depth"`
	Editor   *string                      `yaml:"editor"`
	Mouse    *bool                        `yaml:"mouse"`
	Quick    *bool                        `yaml:"quick_access"`
	Updates  *string                      `yaml:"update_check"`
	Theme    string                       `yaml:"theme"`
	Themes   map[string]map[string]string `yaml:"themes"`
	Colors   map[string]string            `yaml:"colors"`
	Keys     map[string]keyList           `yaml:"keys"`
}

// keyList is one key ("r") or a list of keys ([r, ctrl+r]).
type keyList []string

func (k *keyList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*k = keyList{n.Value}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*k = list
	return nil
}

type rootsFlag []string

func (r *rootsFlag) String() string { return strings.Join(*r, ",") }

func (r *rootsFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*r = append(*r, p)
		}
	}
	return nil
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "lazycompose", "config.yaml")
}

const defaultConfig = `# lazycompose config
#
# This file was created on first run. Delete it to get the defaults back.
# Command line flags (-root, -max-depth, -theme) override these values.

# Folders to scan for compose files. Every folder below a root that has a
# compose.yaml, compose.yml, docker-compose.yaml or docker-compose.yml is
# shown as one environment. "~" is your home folder; keep it in quotes.
roots:
  - "~"
#  - /srv/stacks

# How many folders deep to look under each root. 0 means no limit.
# Hidden folders, node_modules, vendor, testdata and the Go module cache are
# always skipped, and folders inside an environment are not scanned.
max_depth: 0

# Editor for the e key. It can include arguments; graphical editors need a
# flag that waits until the file is closed, like "code --wait".
# Set it to "" to use $VISUAL, then $EDITOR, then nvim or vi.
editor: vim

# Scroll and click with the mouse. While it is on, hold shift to select
# text in most terminals.
mouse: true

# Show up to 5 environments you use most, by how often and how recently, at
# the top of the list. Running an action on an environment counts as a use.
# The history is kept in ~/.local/state/lazycompose/history.json.
quick_access: true

# How often to ask the image registries whether a newer image exists for a
# service, like 6h or 30m. Services with an update get a ↑ marker. Only the
# image digest is fetched, which does not count against Docker Hub's pull
# limit. Private registries are skipped. Set it to 0 to turn it off.
update_check: 6h

# Colour theme. Built in: terminal, onedark, tokyonight, catppuccin, gruvbox
# and nord. "terminal" uses your terminal's own colours; the others use fixed
# colours. The terminal background is never changed.
# You can also use the name of one of your own themes from below.
theme: terminal

# Your own themes. Each one starts from a built-in theme given by "base"
# (terminal if left out) and changes the colours you list. Colours are hex
# values like "#ff9e64" or terminal colour numbers from 0 to 255; quote them.
#
# Colour keys:
#   border      borders of panes that are not focused
#   focus       border and title of the focused pane, keys in the footer
#   text        normal text
#   dim         secondary text and group names
#   green       running services
#   yellow      partly running environments and questions
#   red         errors
#   select_bg   background of the selected row
#   select_fg   text of the selected row
#
# themes:
#   sunset:
#     base: onedark
#     focus: "#ff9e64"
#     select_bg: "#3b3052"

# Change single colours of whichever theme is active, using the same keys.
# colors:
#   focus: "#7aa2f7"

# Key bindings. Give an action one key or a list of keys, or [] to turn it
# off. ctrl+c, esc and enter cannot be rebound. Actions and their defaults:
#
#   move_up: [k, up]          move_down: [j, down]
#   next_pane: tab            prev_pane: shift+tab
#   focus_environments: "1"   focus_services: "2"     focus_logs: "3"
#   filter: /                 problems: "!"
#   edit: e                   edit_env: E
#   up: u                     down: d                 stop_start: s
#   restart: r                recreate: R
#   pull: p                   update_all: U
#   logs: l                   shell: x                folder_shell: X
#   refresh: ctrl+r           help: "?"               quit: q
#   quit_to_folder: Q
#
# keys:
#   recreate: F
#   quit: [q, ctrl+d]
`

func loadConfig(path string) (config, error) {
	editor, mouse, quick, updates := "vim", true, true, "6h"
	cfg := config{Roots: []string{"~"}, Editor: &editor, Mouse: &mouse, Quick: &quick, Updates: &updates, Theme: ui.DefaultTheme}
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// first run: write the defaults so there is a file to edit.
		// Failing to write is fine, the defaults still apply.
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, []byte(defaultConfig), 0o644)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var file config
	if err := yaml.Unmarshal(b, &file); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if len(file.Roots) > 0 {
		cfg.Roots = file.Roots
	}
	cfg.MaxDepth = max(file.MaxDepth, 0)
	if file.Theme != "" {
		cfg.Theme = file.Theme
	}
	// a missing key keeps vim, an empty value means use $VISUAL / $EDITOR
	if file.Editor != nil {
		cfg.Editor = file.Editor
	}
	if file.Mouse != nil {
		cfg.Mouse = file.Mouse
	}
	if file.Quick != nil {
		cfg.Quick = file.Quick
	}
	if file.Updates != nil {
		if _, err := updateEvery(*file.Updates); err != nil {
			return cfg, fmt.Errorf("%s: update_check: %w", path, err)
		}
		cfg.Updates = file.Updates
	}
	cfg.Themes = file.Themes
	cfg.Colors = file.Colors
	cfg.Keys = file.Keys
	return cfg, nil
}

// updateEvery parses update_check: a duration, or 0 / off to turn it off.
func updateEvery(s string) (time.Duration, error) {
	switch strings.TrimSpace(s) {
	case "", "0", "off", "false":
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration like 6h or 30m", s)
	}
	if d < time.Minute {
		return 0, fmt.Errorf("%q is too short, use at least 1m", s)
	}
	return d, nil
}

func expandHome(p string) string {
	// a bare ~ in YAML is null, which arrives here as ""
	if p == "" || p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// version can be set at build time with -ldflags "-X main.version=v1.0.0".
var version string

func versionString() string {
	if version != "" {
		return version
	}
	// go build stamps a version from git, go install from the module tag
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	var roots rootsFlag
	configPath := flag.String("config", defaultConfigPath(), "config file")
	depth := flag.Int("max-depth", -1, "how deep to look for compose files, 0 for no limit (overrides config)")
	theme := flag.String("theme", "", "colour theme: "+strings.Join(ui.ThemeNames(), ", ")+" (overrides config)")
	flag.Var(&roots, "root", "folder to scan, can be repeated or comma separated (overrides config)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	cdFile := flag.String("cd-file", "", "on quit to folder (Q), write the selected folder to this file")
	flag.Parse()

	if *showVersion {
		fmt.Println("lazycompose", versionString())
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	if len(roots) > 0 {
		cfg.Roots = roots
	}
	if *depth >= 0 {
		cfg.MaxDepth = *depth
	}
	if *theme != "" {
		cfg.Theme = *theme
	}
	th, err := ui.LoadTheme(cfg.Theme, cfg.Themes, cfg.Colors)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	overrides := map[string][]string{}
	for name, ks := range cfg.Keys {
		overrides[name] = ks
	}
	if err := ui.SetKeys(overrides); err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	for i, r := range cfg.Roots {
		cfg.Roots[i] = expandHome(r)
	}

	find := func() ([]discover.Env, error) { return discover.Find(cfg.Roots, cfg.MaxDepth) }
	// check the roots up front so a typo gets a clear message; the scan
	// itself runs in the background once the UI is up
	for _, r := range cfg.Roots {
		if _, err := os.Stat(r); err != nil {
			fmt.Fprintln(os.Stderr, "lazycompose:", err)
			if errors.Is(err, fs.ErrNotExist) && *configPath != "" {
				fmt.Fprintf(os.Stderr, "set your folders in %s or pass -root\n", *configPath)
			}
			os.Exit(1)
		}
	}

	every, _ := updateEvery(*cfg.Updates) // checked when loading the config
	host, _ := os.Hostname()
	m := ui.New(ui.Options{
		Hostname:    host,
		Roots:       cfg.Roots,
		Find:        find,
		Theme:       th,
		Editor:      *cfg.Editor,
		CdFile:      *cdFile,
		Mouse:       *cfg.Mouse,
		QuickAccess: *cfg.Quick,
		History:     history.Load(history.DefaultPath()),
		UpdateEvery: every,
	})

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if *cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	final, err := tea.NewProgram(m, opts...).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	if fm, ok := final.(ui.Model); ok && *cdFile != "" {
		if err := os.WriteFile(*cdFile, []byte(fm.QuitDir()), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "lazycompose:", err)
			os.Exit(1)
		}
	}
}
