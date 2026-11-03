# Keys

[README](../README.md) · [Usage](usage.md) · [Configuration](configuration.md) · **Keys** · [Themes](themes.md) · [Shell integration](shell.md) · [Development](development.md)

Lowercase keys act on the selection; the capital of the same letter is the
stronger version (`r` restart, `R` force recreate). Press `?` in lazycompose to
see the keys you have set.

| Key | Action | Name in config |
| --- | --- | --- |
| `tab` / `shift+tab` | Next / previous pane | `next_pane`, `prev_pane` |
| `1` `2` `3` | Jump to environments, services, logs | `focus_environments`, `focus_services`, `focus_logs` |
| `j` `k` / arrows | Move, or scroll the log pane | `move_down`, `move_up` |
| `/` | Filter environments; in the log pane, search the logs | `filter` |
| `!` | Show only environments with problems | `problems` |
| `e` | Edit the compose file | `edit` |
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
| `X` | Open your shell in the environment's folder, see [Shell integration](shell.md#shell-in-the-folder) | `folder_shell` |
| `ctrl+r` | Refresh and rescan folders | `refresh` |
| `?` | Show all keys | `help` |
| `q` | Quit | `quit` |
| `Q` | Quit and leave your shell in the selected folder, see [Shell integration](shell.md#change-directory-on-quit) | `quit_to_folder` |

`u`, `r`, `R`, `p` and `l` act on the whole environment, or on the selected
service when the services pane is focused.

## Fixed keys

These keep their meaning and cannot be rebound: `ctrl+c` always quits, `esc`
cancels or clears (a filter, a search, a running update all), `enter` confirms
input and `y` / `n` answer questions. In the log pane `n` / `N` jump to the
older / newer search match, `g` / `G` to the top / bottom, and page up / down
scroll.

## Mouse

The wheel scrolls the pane under the pointer and a click selects an
environment or service. Turn it off with `mouse: false` in the config.

## Custom keys

Rebind any action under `keys` in the config, using the names from the table
above. Give one key or a list; an empty list turns the action off:

```yaml
keys:
  recreate: F
  quit: [q, ctrl+d]
  refresh: []
```

Keys are written the way the terminal reports them: letters (`a`, `A`),
`ctrl+` combinations (`ctrl+r`), and names like `tab`, `shift+tab`, `up`,
`down`, `pgup` and `f5`.

A key used by two actions, an unknown action name or a fixed key is reported
when lazycompose starts.
