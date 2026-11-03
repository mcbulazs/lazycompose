# lazycompose

[![ci](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml/badge.svg)](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml)

A terminal UI for managing Docker Compose stacks on a server. It scans
your app folders for compose files, shows what is running, and lets you start,
stop, restart, pull, edit and tail logs without leaving the terminal.

_Screenshot coming soon._
<!-- ![lazycompose](docs/screenshot.png) -->

## How it works

Every folder with a `compose.yaml` (or `compose.yml`, `docker-compose.yaml`,
`docker-compose.yml`) under one of the configured roots is an environment:

```
/srv/stacks
├── media/jellyfin/compose.yaml
├── monitoring/grafana/compose.yaml
├── monitoring/prometheus/compose.yaml
└── nextcloud/compose.yaml
```

Environments are grouped by their parent folder. The status dot shows
`●` all services running, `◐` some running, `○` down.

lazycompose runs on the host, not in a container, since it calls
`docker compose` and opens your editor.

## Build

Requires Go 1.27 or newer.

```sh
go install github.com/mcbulazs/lazycompose/cmd/lazycompose@latest
```

Or from a clone:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/lazycompose ./cmd/lazycompose
```

With `CGO_ENABLED=0` the result is a static binary you can copy to another
machine. Install it somewhere on your `PATH`:

```sh
sudo install -m 755 bin/lazycompose /usr/local/bin/lazycompose
```

To stamp a release version into the binary, add
`-X main.version=v0.1.0` to `-ldflags`. Check it with `lazycompose -version`.

Run the tests:

```sh
go test ./...
```

## Config

On first run lazycompose creates `~/.config/lazycompose/config.yaml` with the
defaults and a comment for every option. Edit it to point at your own folders:

```yaml
roots:
  - /srv/stacks
  - ~/projects
max_depth: 0
editor: nvim
theme: terminal
colors:
  focus: "#7aa2f7"
```

| Key | Default | Description |
| --- | --- | --- |
| `roots` | your home folder | Folders to scan for compose files. |
| `max_depth` | `0` | How many folders deep to look. `0` means no limit. |
| `editor` | `vim` | Editor for `e`, e.g. `nvim` or `code --wait`. Set it to `""` to use `$VISUAL`, then `$EDITOR`, then nvim or vi. |
| `theme` | `terminal` | A built-in theme or one of your own. |
| `themes` | | Your own themes, see below. |
| `colors` | | Change single colours of the active theme. |

Folders named `node_modules`, `vendor` or `testdata`, hidden folders and the
Go module cache are skipped. Once a compose file is found in a folder, its
subfolders are not scanned.

### Themes

Built-in themes: `terminal`, `onedark`, `tokyonight`, `catppuccin`, `gruvbox`
and `nord`. The `terminal` theme uses your terminal's own colours, like lazygit
does: a green border on the active pane and a blue selected line. The others
use fixed colours and paint the terminal background while lazycompose runs.

To make your own, add it under `themes` and select it with `theme`. It starts
from the built-in theme in `base` (`terminal` if left out) and changes only the
colours you list:

```yaml
theme: sunset
themes:
  sunset:
    base: onedark
    focus: "#ff9e64"
    select_bg: "#3b3052"
```

Colours are hex values or terminal colour numbers (0-255).

| Key | Used for |
| --- | --- |
| `border` | Borders of panes that are not focused |
| `focus` | Border and title of the focused pane, keys in the footer |
| `text` | Normal text |
| `dim` | Secondary text and group names |
| `green` | Running services |
| `yellow` | Partly running environments and questions |
| `red` | Errors |
| `select_bg` | Background of the selected row |
| `select_fg` | Text of the selected row |
| `background` | Terminal background, hex only |

Flags override the config:

| Flag | Description |
| --- | --- |
| `-root DIR` | Folder to scan. Repeat or comma separate for several. |
| `-max-depth N` | How many folders deep to look, `0` for no limit. |
| `-theme NAME` | Colour theme. |
| `-config FILE` | Use a different config file. |
| `-version` | Print the version and exit. |

## Keys

| Key | Action |
| --- | --- |
| `tab` / `shift+tab` | Switch pane (environments, services, logs) |
| `j` `k` / arrows | Move, or scroll the log pane |
| `/` | Filter environments, `enter` to keep, `esc` to clear |
| `e` | Edit the compose file (see `editor` in the config) |
| `u` | `docker compose up -d` |
| `d` | `docker compose down` (asks first) |
| `r` | `docker compose restart` |
| `p` | `docker compose pull` |
| `l` | Follow logs |
| `x` | Open a shell in the selected service (bash if it has it, otherwise sh) |
| `esc` | Stop following logs |
| `R` | Refresh and rescan folders |
| `?` | Show all keys |
| `q` | Quit |

`u`, `r`, `p` and `l` act on the whole environment, or on the selected
service when the services pane is focused.

After editing, lazycompose runs `docker compose config -q`. If the file is
valid it asks whether to apply the changes with `up -d`; if not, the error is
shown in the log pane.

Status refreshes every 3 seconds.

## License

MIT, see [LICENSE](LICENSE).
