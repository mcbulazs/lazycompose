# Shell integration

[README](../README.md) · [Usage](usage.md) · [Configuration](configuration.md) · [Keys](keys.md) · [Themes](themes.md) · **Shell integration** · [Development](development.md)

## Change directory on quit

`Q` quits lazycompose and leaves your shell in the selected environment's
folder.

A program cannot change the folder of the shell that started it, so `Q`
writes the folder to the file given with `-cd-file` and a small shell function
does the `cd`. Without `-cd-file`, `Q` only shows a hint. After a plain `q` the
file is empty and you stay where you were.

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

Then start it with `lc` instead of `lazycompose`. Any flags are passed on, for
example `lc -root /srv/stacks`.

## Shell in the folder

`X` opens your shell in the selected environment's folder without quitting.

- Inside tmux it opens a new tmux window in that folder, named after the
  environment, and lazycompose keeps running in its own window.
- Outside tmux it starts a nested shell (`$SHELL`, or `sh`). Type `exit` to
  come back; lazycompose then refreshes, in case you changed the compose file.

## Shell in a container

`x` opens a shell inside the selected service's container
(`docker compose exec <service> bash`, or `sh` when the image has no bash).
Exit it to come back to lazycompose.
