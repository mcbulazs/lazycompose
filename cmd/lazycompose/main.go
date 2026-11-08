package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/compose"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/history"
	"github.com/mcbulazs/lazycompose/internal/remote"
	"github.com/mcbulazs/lazycompose/internal/selfupdate"
	"github.com/mcbulazs/lazycompose/internal/stackfs"
	"github.com/mcbulazs/lazycompose/internal/ui"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Roots    []string                     `yaml:"roots"`
	MaxDepth int                          `yaml:"max_depth"`
	Editor   *string                      `yaml:"editor"`
	Mouse    *bool                        `yaml:"mouse"`
	Confirm  *bool                        `yaml:"confirm"`
	After    string                       `yaml:"after_edit"`
	Quick    *bool                        `yaml:"quick_access"`
	Version  *bool                        `yaml:"version_check"`
	Updates  *string                      `yaml:"update_check"`
	Template string                       `yaml:"new_env_template"`
	Dotenv   bool                         `yaml:"new_env_create_dotenv"`
	Filename string                       `yaml:"new_compose_filename"`
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

// defaultConfigPath is $XDG_CONFIG_HOME/lazycompose/config.yaml, or
// ~/.config/lazycompose/config.yaml, on macOS too, where terminal tools keep
// their config there rather than in ~/Library. On Windows it is in
// %AppData%.
func defaultConfigPath() string {
	dir, err := configDir(runtime.GOOS)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "lazycompose", "config.yaml")
}

func configDir(goos string) (string, error) {
	if goos == "windows" {
		return os.UserConfigDir()
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
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
#  - ssh://user@server/srv/stacks   # over ssh, see the wiki on remote roots

# How many folders deep to look under each root. 0 means no limit.
# Hidden folders, node_modules, vendor, testdata and the Go module cache are
# always skipped, and folders inside an environment are not scanned.
max_depth: 0

# Editor for the e key. It can include arguments. Graphical editors like
# "code" can stay open: every save is checked and offered to apply.
# Set it to "" to use $VISUAL, then $EDITOR, then nvim or vi (or notepad on
# Windows, where it is "" to begin with).
editor: vim

# Scroll and click with the mouse. While it is on, hold shift to select
# text in most terminals.
mouse: true

# Ask before actions that stop or change containers: down, restart, force
# recreate, update all, removing old images, moving, and actions on a
# folder. false runs them right away.
confirm: true

# After an edit is saved and checked: ask whether to apply it with up -d
# (ask), apply it right away (apply), or only check it (check).
after_edit: ask

# Show up to 5 environments you use most, by how often and how recently, at
# the top of the list. Running an action on an environment counts as a use.
# Environments pinned with f always show first, with a star, even when this
# is off. Uses and pins are kept in ~/.local/state/lazycompose/history.json.
quick_access: true

# How often to ask the image registries whether a newer image exists for a
# service, like 6h or 30m. Services with an update get a ↑ marker. Only the
# image digest is fetched, which does not count against Docker Hub's pull
# limit. Private registries are skipped. Set it to 0 to turn it off.
update_check: 6h

# Once a day, look on GitHub for a newer lazycompose release and mention it
# in the header. lazycompose -update installs it.
version_check: true

# The compose file name of new environments (the n key): compose.yaml,
# compose.yml, docker-compose.yaml or docker-compose.yml.
new_compose_filename: docker-compose.yml

# New environments get their compose file from this file. Left out, they get
# a starter with one service. "~" works here too.
# new_env_template: ~/stacks/template.yml

# Also create an empty .env file in new environments.
new_env_create_dotenv: false

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
#   edit: e                   edit_env: E             edit_config: c
#   new_env: n                move_env: m             pin: f
#   up: u                    down: d                 stop_start: s
#   restart: r                recreate: R             open_logs: o
#   pull: p                   update_all: U           clean_images: P
#   logs: l                   shell: x                folder_shell: X
#   zoom: z                   compose_view: v         refresh: ctrl+r
#   help: "?"                 commands: ":"
#   quit: q                   quit_to_folder: Q
#
# keys:
#   recreate: F
#   quit: [q, ctrl+d]
`

// defaultEditor is vim, which Linux and macOS have. Windows usually has
// not, so there the editor is found like when it is set to "".
func defaultEditor() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return "vim"
}

// firstConfig is the config file written on first run, with the default
// editor filled in.
func firstConfig(editor string) string {
	if editor == "vim" {
		return defaultConfig
	}
	return strings.Replace(defaultConfig, "\neditor: vim\n", "\neditor: "+strconv.Quote(editor)+"\n", 1)
}

// checkConfig reads the config file at path like a start would, to report
// mistakes after it was edited.
func checkConfig(path string) error {
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	if _, err := ui.LoadTheme(cfg.Theme, cfg.Themes, cfg.Colors); err != nil {
		return err
	}
	return ui.CheckKeys(keyOverrides(cfg.Keys))
}

func keyOverrides(keys map[string]keyList) map[string][]string {
	out := map[string][]string{}
	for name, ks := range keys {
		out[name] = ks
	}
	return out
}

func loadConfig(path string) (config, error) {
	editor, mouse, quick, updates, versions, confirm := defaultEditor(), true, true, "6h", true, true
	cfg := config{Roots: []string{"~"}, Editor: &editor, Mouse: &mouse, Confirm: &confirm, After: ui.AfterEditAsk,
		Quick: &quick, Updates: &updates, Version: &versions, Filename: stackfs.ComposeName, Theme: ui.DefaultTheme}
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// first run: write the defaults so there is a file to edit.
		// Failing to write is fine, the defaults still apply.
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, []byte(firstConfig(defaultEditor())), 0o644)
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
	if file.Confirm != nil {
		cfg.Confirm = file.Confirm
	}
	switch file.After {
	case "":
	case ui.AfterEditAsk, ui.AfterEditApply, ui.AfterEditCheck:
		cfg.After = file.After
	default:
		return cfg, fmt.Errorf("%s: after_edit: %q is not ask, apply or check", path, file.After)
	}
	if file.Quick != nil {
		cfg.Quick = file.Quick
	}
	if file.Version != nil {
		cfg.Version = file.Version
	}
	if file.Template != "" {
		cfg.Template = expandHome(file.Template)
	}
	cfg.Dotenv = file.Dotenv
	if file.Filename != "" {
		// another name would not be found by the scan
		if !slices.Contains(discover.ComposeFiles, file.Filename) {
			return cfg, fmt.Errorf("%s: new_compose_filename: %q is not one of %s",
				path, file.Filename, strings.Join(discover.ComposeFiles, ", "))
		}
		cfg.Filename = file.Filename
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

// selfUpdate replaces this binary with the latest release.
// updateHint says how to update in the header: with -update, or with the
// package manager that installed the binary.
func updateHint() string {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil && selfupdate.Managed(exe) {
		return "update it with your package manager"
	}
	return ""
}

func selfUpdate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	current := versionString()
	fmt.Printf("lazycompose %s at %s, looking for a newer release…\n", current, exe)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tag, err := selfupdate.New().Update(ctx, current, exe)
	if errors.Is(err, selfupdate.ErrUpToDate) {
		fmt.Printf("already the latest release (%s)\n", tag)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("updated to %s\n", tag)
	return nil
}

func main() {
	// started by ssh to ask a login question: hand it to the lazycompose
	// that started ssh, and do nothing else
	if code, ok := remote.RunAskpass(os.Args); ok {
		os.Exit(code)
	}
	var roots rootsFlag
	configPath := flag.String("config", defaultConfigPath(), "config file")
	depth := flag.Int("max-depth", -1, "how deep to look for compose files, 0 for no limit (overrides config)")
	theme := flag.String("theme", "", "colour theme: "+strings.Join(ui.ThemeNames(), ", ")+" (overrides config)")
	flag.Var(&roots, "root", "folder to scan, can be repeated or comma separated (overrides config)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	update := flag.Bool("update", false, "update lazycompose to the latest release and exit")
	cdFile := flag.String("cd-file", "", "on quit to folder (Q), write the selected folder to this file")
	flag.Parse()

	if exe, err := os.Executable(); err == nil {
		selfupdate.RemoveOld(exe) // what an update on Windows moved aside
	}
	if *showVersion {
		fmt.Println("lazycompose", versionString())
		return
	}
	if *update {
		if err := selfUpdate(); err != nil {
			fmt.Fprintln(os.Stderr, "lazycompose:", err)
			os.Exit(1)
		}
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
	if err := ui.SetKeys(keyOverrides(cfg.Keys)); err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	local := false
	for i, r := range cfg.Roots {
		if remote.IsRemote(r) {
			continue
		}
		cfg.Roots[i] = expandHome(r)
		local = true
	}

	find := func(roots []string) ([]discover.Env, error) { return discover.FindRoots(roots, cfg.MaxDepth) }
	// check the roots up front so a typo gets a clear message; the scan
	// itself runs in the background once the UI is up, where a remote
	// root that cannot be reached does not stop the others
	for _, r := range cfg.Roots {
		if remote.IsRemote(r) {
			if _, _, err := remote.Parse(r); err != nil {
				fmt.Fprintln(os.Stderr, "lazycompose:", err)
				os.Exit(1)
			}
			continue
		}
		if _, err := os.Stat(r); err != nil {
			fmt.Fprintln(os.Stderr, "lazycompose:", err)
			if errors.Is(err, fs.ErrNotExist) && *configPath != "" {
				fmt.Fprintf(os.Stderr, "set your folders in %s or pass -root\n", *configPath)
			}
			os.Exit(1)
		}
	}
	// with only remote roots, docker does not have to be installed here
	if local {
		if err := compose.Check(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "lazycompose:", err)
			os.Exit(1)
		}
	}
	if p := history.DefaultPath(); p != "" {
		// next to the history; the folder is made private when first used
		remote.ControlDir = filepath.Join(filepath.Dir(p), "ssh")
	}

	ui.RemoveLeftovers()
	every, _ := updateEvery(*cfg.Updates) // checked when loading the config
	var latestVersion func(context.Context) (string, error)
	if *cfg.Version {
		latestVersion = selfupdate.New().Latest
	}
	host, _ := os.Hostname()
	m := ui.New(ui.Options{
		Hostname:    host,
		Roots:       cfg.Roots,
		Find:        find,
		Theme:       th,
		Editor:      *cfg.Editor,
		CdFile:      *cdFile,
		Mouse:       *cfg.Mouse,
		NoConfirm:   !*cfg.Confirm,
		AfterEdit:   cfg.After,
		QuickAccess: *cfg.Quick,
		History:     history.Load(history.DefaultPath()),
		UpdateEvery: every,
		MaxDepth:    cfg.MaxDepth,
		NewTemplate: cfg.Template,
		NewDotenv:   cfg.Dotenv,

		NewComposeFilename: cfg.Filename,
		Version:            versionString(),
		LatestVersion:      latestVersion,
		UpdateHint:         updateHint(),
		ConfigPath:         *configPath,
		CheckConfig:        func() error { return checkConfig(*configPath) },
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
