# Using lazycompose

[README](../README.md) · **Usage** · [Configuration](configuration.md) · [Keys](keys.md) · [Themes](themes.md) · [Shell integration](shell.md) · [Development](development.md)

## Environments

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
`●` all services running, `◐` some running, `○` down, followed by the number of
running services out of the total.

lazycompose runs on the host, not in a container, since it calls
`docker compose` and opens your editor. The user running it needs access to
Docker (the `docker` group, or root).

## Problems

Environments with a problem are shown in red, and the header counts them. A
problem is a container that is unhealthy, restarting in a loop, or exited with
an error. Exit codes 137 and 143 are left by a normal stop, so a stack you
stopped yourself does not count. Press `!` to show only environments with
problems.

## Services

The services table shows each container's state, health, CPU and memory use,
ports, image and uptime. Services that are defined but have no container are
listed as `not created`. CPU and memory are sampled every few seconds for the
selected environment only. On a narrow terminal the less important columns
are hidden.

Most actions work on the whole environment, or on the selected service when
the services pane is focused: `u` up, `r` restart, `R` force recreate, `p`
pull and `l` logs.

## Logs and command output

The log pane follows the selected environment, or the selected service while
the services pane is focused. Logs are fetched once the selection rests for a
moment, so scrolling through the list stays quick.

While a command like `up -d` or `pull` runs, the pane shows its output live
instead, and keeps it after the command finishes until you press a key.
Switching panes and scrolling the log pane do not count, so you can read the
output first.

In the log pane, `/` searches. `n` and `N` jump to the older and newer match,
`esc` clears the search, and `g` / `G` jump to the top / bottom.

## Editing

`e` opens the compose file and `E` the `.env` file next to it in your editor
(see `editor` in the [configuration](configuration.md)). When you close the
editor and the file changed, lazycompose runs `docker compose config -q`. If
the file is valid it asks whether to apply the changes with `up -d`; if not,
the error is shown in the log pane.

## Update all

`U` updates every environment that has at least one running service: `pull`,
then `up -d`, one environment at a time. Each gets one line in the log pane:
`updated` (containers were recreated), `up to date` or `failed` with Docker's
error. `esc` stops after the current environment. Environments that are down
are skipped, since `up -d` would start them.

## Status updates

The status updates as soon as Docker reports a change (from `docker events`),
with a check every 15 seconds that keeps uptimes current. If events are not
available it checks every 3 seconds instead.
