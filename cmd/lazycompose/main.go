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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/ui"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Roots    []string                     `yaml:"roots"`
	MaxDepth int                          `yaml:"max_depth"`
	Editor   *string                      `yaml:"editor"`
	Theme    string                       `yaml:"theme"`
	Themes   map[string]map[string]string `yaml:"themes"`
	Colors   map[string]string            `yaml:"colors"`
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

# Colour theme. Built in: terminal, onedark, tokyonight, catppuccin, gruvbox
# and nord. "terminal" uses your terminal's own colours; the others use fixed
# colours and paint the terminal background while lazycompose runs.
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
#   background  terminal background, hex only
#
# themes:
#   sunset:
#     base: onedark
#     focus: "#ff9e64"
#     select_bg: "#3b3052"

# Change single colours of whichever theme is active, using the same keys.
# colors:
#   focus: "#7aa2f7"
`

func loadConfig(path string) (config, error) {
	editor := "vim"
	cfg := config{Roots: []string{"~"}, Editor: &editor, Theme: ui.DefaultTheme}
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
	cfg.Themes = file.Themes
	cfg.Colors = file.Colors
	return cfg, nil
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

	host, _ := os.Hostname()
	m := ui.New(ui.Options{
		Hostname: host,
		Roots:    cfg.Roots,
		Find:     find,
		Theme:    th,
		Editor:   *cfg.Editor,
	})

	// paint the terminal background while running
	if th.Background != "" {
		fmt.Print("\x1b]11;" + th.Background + "\x07")
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	if th.Background != "" {
		fmt.Print("\x1b]111\x07")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
}
