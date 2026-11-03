# lazycompose

[![ci](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml/badge.svg)](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml)

A terminal UI for managing Docker Compose stacks on a server. It scans
your app folders for compose files, shows what is running, and lets you start,
stop, restart, pull, edit and tail logs without leaving the terminal.

![lazycompose showing environments, services and live logs](docs/screenshot.png)

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

The services table shows each container's state, health, CPU and memory use,
ports, image and uptime. CPU and memory are sampled every few seconds for the
selected environment only. On a narrow terminal the less important columns
are hidden.

Environments with a problem are shown in red, and the header counts them. A
problem is a container that is unhealthy, restarting in a loop, or exited with
an error. Exit codes 137 and 143 are left by a normal stop, so a stack you
stopped yourself does not count.

lazycompose runs on the host, not in a container, since it calls
`docker compose` and opens your editor.

## Install

Download a release binary (Linux amd64; use `arm64` for ARM boards):

```sh
curl -fsSL https://github.com/mcbulazs/lazycompose/releases/latest/download/lazycompose_linux_amd64.tar.gz | tar -xz lazycompose
sudo install -m 755 lazycompose /usr/local/bin/lazycompose
```

Each release also has a `checksums.txt` with SHA-256 sums.

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
| `mouse` | `true` | Scroll and click with the mouse. Hold shift to select text while it is on. |
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
use fixed colours. None of them change the terminal background, so transparent
terminals keep working.

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

Flags override the config:

| Flag | Description |
| --- | --- |
| `-root DIR` | Folder to scan. Repeat or comma separate for several. |
| `-max-depth N` | How many folders deep to look, `0` for no limit. |
| `-theme NAME` | Colour theme. |
| `-config FILE` | Use a different config file. |
| `-version` | Print the version and exit. |
| `-cd-file FILE` | Where `Q` writes the selected folder, see [Change directory on quit](#change-directory-on-quit). |

## Keys

Lowercase keys act on the selection; the capital of the same letter is the
stronger version (`r` restart, `R` force recreate).

| Key | Action | Name in config |
| --- | --- | --- |
| `tab` / `shift+tab` | Next / previous pane | `next_pane`, `prev_pane` |
| `1` `2` `3` | Jump to environments, services, logs | `focus_environments`, `focus_services`, `focus_logs` |
| `j` `k` / arrows | Move, or scroll the log pane | `move_down`, `move_up` |
| `/` | Filter environments; in the log pane, search the logs | `filter` |
| `!` | Show only environments with problems | `problems` |
| `e` | Edit the compose file (see `editor` in the config) | `edit` |
| `E` | Edit the environment's `.env` file, created if it does not exist yet | `edit_env` |
| `u` | `docker compose up -d` | `up` |
| `d` | `docker compose down` (asks first) | `down` |
| `s` | Stop the selected service, or start it if it is not running | `stop_start` |
| `r` | `docker compose restart` | `restart` |
| `R` | `docker compose up -d --force-recreate` (asks first) | `recreate` |
| `p` | `docker compose pull` | `pull` |
| `U` | Update all running environments: `pull`, then `up -d`, one at a time (asks first) | `update_all` |
| `l` | Fetch the logs again | `logs` |
| `x` | Open a shell in the selected service (bash if it has it, otherwise sh) | `shell` |
| `X` | Open your shell in the environment's folder: a new tmux window when running inside tmux, otherwise a nested shell (`exit` to come back) | `folder_shell` |
| `ctrl+r` | Refresh and rescan folders | `refresh` |
| `?` | Show all keys | `help` |
| `q` | Quit | `quit` |
| `Q` | Quit and leave your shell in the selected environment's folder | `quit_to_folder` |

These keys are fixed: `ctrl+c` always quits, `esc` cancels or clears (a
filter, a search, a running update all), `enter` confirms input, `y` / `n`
answer questions, and in the log pane `n` / `N` jump to the older / newer
search match, `g` / `G` to the top / bottom and page up / down scroll.

### Custom keys

Rebind any action under `keys` in the config, with one key or a list.
An empty list turns the action off:

```yaml
keys:
  recreate: F
  quit: [q, Q]
  refresh: []
```

A key used by two actions, or an unknown action name, is reported when
lazycompose starts.

The mouse works too: the wheel scrolls the pane under the pointer and a click
selects an environment or service.

The log pane follows the selected environment, or the selected service while
the services pane is focused. While a command like `up -d` or `pull` runs, the
pane shows its output live instead, and keeps it after the command finishes
until you press a key.

`u`, `r`, `R`, `p` and `l` act on the whole environment, or on the selected
service when the services pane is focused.

After editing, lazycompose runs `docker compose config -q`. If the file is
valid it asks whether to apply the changes with `up -d`; if not, the error is
shown in the log pane.

Status updates as soon as Docker reports a change (from `docker events`), with a check every 15 seconds. If events are not available it checks every 3 seconds instead.

## Change directory on quit

A program cannot change the folder of the shell that started it, so `Q`
writes the selected environment's folder to the file given with `-cd-file`
and a small shell function does the `cd`. Without `-cd-file`, `Q` only shows
a hint. After a plain `q` the file is empty and you stay where you were.

bash or zsh, in `~/.bashrc` or `~/.zshrc`:

```sh
lc() {
    local tmp dir
    tmp="$(mktemp)"
    lazycompose -cd-file "$tmp" "$@"
    dir="$(cat "$tmp")"
    rm -f "$tmp"
    if [ -n "$dir" ] && [ -d "$dir" ]; then
        cd "$dir"
    fi
}
```

fish, in `~/.config/fish/functions/lc.fish`:

```fish
function lc
    set -l tmp (mktemp)
    lazycompose -cd-file $tmp $argv
    set -l dir (cat $tmp)
    rm -f $tmp
    if test -n "$dir"; and test -d "$dir"
        cd $dir
    end
end
```

Then start it with `lc` instead of `lazycompose`.

## License

MIT, see [LICENSE](LICENSE).

## Releasing

Push a tag starting with `v` and GitHub Actions builds the Linux binaries and
publishes a release:

```sh
git tag v0.1.0
git push origin v0.1.0
```
