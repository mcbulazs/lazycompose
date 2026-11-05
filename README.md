# lazycompose

[![ci](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml/badge.svg)](https://github.com/mcbulazs/lazycompose/actions/workflows/ci.yml)

A terminal UI for managing Docker Compose stacks on a server. It scans
your app folders for compose files, shows what is running, and lets you start,
stop, restart, pull, edit and tail logs without leaving the terminal.

![lazycompose showing environments, services and live logs](docs/screenshot.png)

- Every folder with a compose file is an environment, grouped by parent folder
- Live status from `docker events`, with unhealthy and crashed stacks in red
- Services with state, health, CPU, memory, ports, image and uptime
- Logs that follow the selection, with search, or the compose file instead
- Edit the compose or `.env` file, validated before it is applied
- Update all running stacks with one key, and a `↑` on services with a newer image
- Quick access to the stacks you use most, and pins for the ones you always want
- Create, move and rename stacks from the app
- Themes, configurable keys, mouse support

## Install

On Linux (x86_64 or ARM64):

```sh
curl -fsSL https://raw.githubusercontent.com/mcbulazs/lazycompose/main/install.sh | sh
```

It downloads the release for your machine, checks it against the release's
`checksums.txt` and installs it into `/usr/local/bin`, using `sudo` if
needed. Add `-s -- -b ~/.local/bin` after `sh` to install somewhere else, or
`-s -- -v v0.4.0` for a given version. You can read
[install.sh](install.sh) first.

To update later, run `sudo lazycompose -update` (without `sudo` if the binary
is somewhere you can write). It downloads the latest release, checks it
against `checksums.txt` and replaces the binary. lazycompose also mentions a
new release in its header, once a day.

By hand: download `lazycompose_linux_amd64.tar.gz` (or `arm64`) from the
[latest release](https://github.com/mcbulazs/lazycompose/releases/latest),
unpack it and put `lazycompose` in your `PATH`.

Or with Go 1.27 or newer:

```sh
go install github.com/mcbulazs/lazycompose/cmd/lazycompose@latest
```

## Quick start

Point it at the folder that holds your stacks:

```sh
lazycompose -root /srv/stacks
```

The first run creates `~/.config/lazycompose/config.yaml`, with a comment for
every option. Set `roots` there to make the folder permanent.

## Keys

| Key | Action |
| --- | --- |
| `j` `k` / arrows, `tab` | Move, switch pane |
| `u` / `d` | `up -d` / `down` |
| `r` / `R` | Restart / force recreate |
| `p` / `U` | Pull / update all running stacks |
| `P` | Remove old images left behind by pulls (asks first) |
| `z` | Zoom the focused pane to full screen |
| `v` | Show the compose file in the log pane, or the logs again |
| `n` / `m` | New environment / move or rename it |
| `f` | Pin to the top of the list |
| `e` / `E` | Edit the compose file / `.env` |
| `x` / `X` | Shell in the container / in the folder |
| `/` | Filter, or search the logs |
| `q` / `Q` | Quit / quit to the folder |
| `?` | Search all keys and the commands they run |
| `:` | Search for an action and run it |

Everything also works with the mouse: click a pane, a row, `? keys` or
`: run` at the bottom, and the yes / no of a question.

## Documentation

The full documentation is in the [wiki](https://github.com/mcbulazs/lazycompose/wiki):

- [Usage](https://github.com/mcbulazs/lazycompose/wiki/Usage): environments, problems, logs, editing, update all
- [Configuration](https://github.com/mcbulazs/lazycompose/wiki/Configuration): the config file and flags
- [Keys](https://github.com/mcbulazs/lazycompose/wiki/Keys): every key, and how to change them
- [Themes](https://github.com/mcbulazs/lazycompose/wiki/Themes): built-in and custom themes
- [Shell integration](https://github.com/mcbulazs/lazycompose/wiki/Shell-integration): change directory on quit, tmux
- [Development](https://github.com/mcbulazs/lazycompose/wiki/Development): building, tests, releasing

## License

MIT, see [LICENSE](LICENSE).
