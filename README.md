# lazycompose

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

Run the tests:

```sh
go test ./...
```

## Config

On first run lazycompose creates `~/.config/lazycompose/config.yaml` with the
defaults. Edit it to point at your own folders:

```yaml
roots:
  - /srv/stacks
  - ~/projects
max_depth: 0
theme: onedark
colors:
  focus: "#7aa2f7"
```

| Key | Default | Description |
| --- | --- | --- |
| `roots` | your home folder | Folders to scan for compose files. |
| `max_depth` | `0` | How many folders deep to look. `0` means no limit. |
| `theme` | `onedark` | `onedark`, `tokyonight`, `catppuccin`, `gruvbox` or `nord`. |
| `colors` | | Override single colours with a hex value or an ANSI number (0-255). |

Themes paint the terminal background while lazycompose runs and reset it on
exit.

Colour keys: `border`, `focus`, `text`, `dim`, `green`, `yellow`, `red`,
`select_bg`, `select_fg` and `background`.

Folders named `node_modules`, `vendor` or `testdata`, hidden folders and the
Go module cache are skipped. Once a compose file is found in a folder, its
subfolders are not scanned.

Flags override the config:

| Flag | Description |
| --- | --- |
| `-root DIR` | Folder to scan. Repeat or comma separate for several. |
| `-max-depth N` | How many folders deep to look, `0` for no limit. |
| `-theme NAME` | Colour theme. |
| `-config FILE` | Use a different config file. |

## Keys

| Key | Action |
| --- | --- |
| `tab` / `shift+tab` | Switch pane (environments, services, logs) |
| `j` `k` / arrows | Move, or scroll the log pane |
| `/` | Filter environments, `enter` to keep, `esc` to clear |
| `e` | Edit the compose file in `$EDITOR` (falls back to nvim, then vi) |
| `u` | `docker compose up -d` |
| `d` | `docker compose down` (asks first) |
| `r` | `docker compose restart` |
| `p` | `docker compose pull` |
| `l` | Follow logs |
| `x` | Open a shell in the selected service (`exec <svc> sh`) |
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
