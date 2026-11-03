# Configuration

[README](../README.md) · [Usage](usage.md) · **Configuration** · [Keys](keys.md) · [Themes](themes.md) · [Shell integration](shell.md) · [Development](development.md)

On first run lazycompose creates `~/.config/lazycompose/config.yaml` with the
defaults and a comment for every option. It never overwrites an existing file;
delete it to get a fresh commented copy.

```yaml
roots:
  - /srv/stacks
  - ~/projects
max_depth: 0
editor: nvim
mouse: true
theme: terminal
```

| Key | Default | Description |
| --- | --- | --- |
| `roots` | your home folder | Folders to scan for compose files. `~` is your home folder. |
| `max_depth` | `0` | How many folders deep to look under each root. `0` means no limit. |
| `editor` | `vim` | Editor for `e` and `E`, e.g. `nvim` or `code --wait`. Set it to `""` to use `$VISUAL`, then `$EDITOR`, then nvim or vi. |
| `mouse` | `true` | Scroll and click with the mouse. Hold shift to select text while it is on. |
| `theme` | `terminal` | A built-in theme or one of your own, see [Themes](themes.md). |
| `themes` | | Your own themes, see [Themes](themes.md). |
| `colors` | | Change single colours of the active theme, see [Themes](themes.md). |
| `keys` | | Rebind keys, see [Keys](keys.md#custom-keys). |

## Editor

The editor can include arguments. Graphical editors need a flag that waits
until the file is closed, otherwise lazycompose sees no change:

```yaml
editor: "code --wait"
```

## Scanning

Each folder below a root that has a compose file is one environment. Folders
inside an environment are not scanned, so app data folders are left alone.
Hidden folders, `node_modules`, `vendor`, `testdata` and the Go module cache
are always skipped.

A large root with no depth limit can take a moment to scan; lazycompose opens
right away and fills the list in when the scan is done.

## Flags

Flags override the config:

| Flag | Description |
| --- | --- |
| `-root DIR` | Folder to scan. Repeat or comma separate for several. |
| `-max-depth N` | How many folders deep to look, `0` for no limit. |
| `-theme NAME` | Colour theme. |
| `-config FILE` | Use a different config file. |
| `-cd-file FILE` | Where `Q` writes the selected folder, see [Shell integration](shell.md). |
| `-version` | Print the version and exit. |
