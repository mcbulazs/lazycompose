package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mcbulazs/lazycompose/internal/discover"
	"github.com/mcbulazs/lazycompose/internal/ui"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Roots    []string          `yaml:"roots"`
	MaxDepth int               `yaml:"max_depth"`
	Theme    string            `yaml:"theme"`
	Colors   map[string]string `yaml:"colors"`
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

const defaultConfig = `# Folders to scan for compose files.
roots:
  - "~"

# How many folders deep to look under each root. 0 means no limit.
max_depth: 0

# Colour theme: terminal, onedark, tokyonight, catppuccin, gruvbox or nord.
# "terminal" uses your terminal's own colours.
theme: terminal

# Override single colours with a hex value or an ANSI number (0-255).
# Keys: border, focus, text, dim, green, yellow, red, select_bg, select_fg,
# background
# colors:
#   focus: "#7aa2f7"
`

func loadConfig(path string) (config, error) {
	cfg := config{Roots: []string{"~"}, Theme: ui.DefaultTheme}
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

func main() {
	var roots rootsFlag
	configPath := flag.String("config", defaultConfigPath(), "config file")
	depth := flag.Int("max-depth", -1, "how deep to look for compose files, 0 for no limit (overrides config)")
	theme := flag.String("theme", "", "colour theme: "+strings.Join(ui.ThemeNames(), ", ")+" (overrides config)")
	flag.Var(&roots, "root", "folder to scan, can be repeated or comma separated (overrides config)")
	flag.Parse()

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
	th, err := ui.LoadTheme(cfg.Theme, cfg.Colors)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		os.Exit(1)
	}
	for i, r := range cfg.Roots {
		cfg.Roots[i] = expandHome(r)
	}

	find := func() ([]discover.Env, error) { return discover.Find(cfg.Roots, cfg.MaxDepth) }
	envs, err := find()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazycompose:", err)
		if errors.Is(err, fs.ErrNotExist) && *configPath != "" {
			fmt.Fprintf(os.Stderr, "set your folders in %s or pass -root\n", *configPath)
		}
		os.Exit(1)
	}

	host, _ := os.Hostname()
	m := ui.New(ui.Options{
		Hostname: host,
		Roots:    cfg.Roots,
		Envs:     envs,
		Find:     find,
		Theme:    th,
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
