# Development

[README](../README.md) · [Usage](usage.md) · [Configuration](configuration.md) · [Keys](keys.md) · [Themes](themes.md) · [Shell integration](shell.md) · **Development**

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
machine:

```sh
sudo install -m 755 bin/lazycompose /usr/local/bin/lazycompose
```

To stamp a version into the binary, add `-X main.version=v0.1.0` to
`-ldflags`. Check it with `lazycompose -version`.

## Tests

```sh
go test ./...
```

The tests need neither Docker nor a terminal: `docker compose` output is read
from fixtures in `internal/compose/testdata`, and the UI is driven by sending
key presses and Docker results to the model.

## Layout

| Package | What it does |
| --- | --- |
| `cmd/lazycompose` | Flags, config file, starting the UI |
| `internal/discover` | Finding environments under the roots |
| `internal/compose` | Running `docker compose` and parsing its JSON |
| `internal/ui` | The Bubble Tea models, keys and themes |

## Releasing

Push a tag starting with `v` and GitHub Actions builds the Linux binaries
(amd64 and arm64) and publishes a release with checksums:

```sh
git tag v0.1.0
git push origin v0.1.0
```
