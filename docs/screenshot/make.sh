#!/bin/sh
# Makes docs/screenshot.png and docs/demo.gif, the screenshot and the demo
# in the README:
#
#   docs/screenshot/make.sh        the screenshot
#   docs/screenshot/make.sh gif    the demo GIF
#   docs/screenshot/make.sh all    both
#
# It starts the demo stacks in testdata/ (alpine containers that print
# realistic logs, under the image names of real apps) and runs lazycompose
# against them in a container with the hostname "homeserver". For the
# screenshot it captures the screen with tmux and renders it with headless
# Chromium; the GIF is recorded by vhs in its Docker image, from demo.tape.
#
# Needs: docker and go, plus tmux, python3, chromium and ImageMagick (magick)
# for the screenshot.
#
# The demo projects are all called shot-*, so they cannot touch your own
# stacks. Image names that already exist are used as they are; only the
# tags the script made are removed afterwards.
set -eu

mode=${1:-png}
case $mode in
png | gif | all) ;;
*)
	echo "usage: make.sh [png|gif|all]" >&2
	exit 1
	;;
esac

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
session=lazycompose-shot
stacks="$work/stacks"
dirs="pihole media/jellyfin media/arr monitoring/stack cloud/nextcloud"
images="pihole/pihole:2024.07 jellyfin/jellyfin:10.10 lscr.io/linuxserver/sonarr:4.0
lscr.io/linuxserver/radarr:5.14 lscr.io/linuxserver/prowlarr:1.25 grafana/grafana:11.3
prom/prometheus:v2.55 prom/node-exporter:v1.8 nextcloud:30-apache postgres:16-alpine
redis:7-alpine vaultwarden/server:1.32 itzg/minecraft-server:java21"
tagged=""
vhs=ghcr.io/charmbracelet/vhs:v0.12.1
recorder=lazycompose-shot-vhs

compose() { # compose DIR ARGS...: docker compose for a demo stack
	d=$1
	shift
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v "$stacks:/srv/stacks" \
		-w "/srv/stacks/$d" docker:cli docker compose "$@"
}

cleanup() {
	tmux kill-session -t "$session" 2>/dev/null || true
	for d in $dirs; do compose "$d" down -t 0 >/dev/null 2>&1 || true; done
	for img in $tagged; do docker rmi "$img" >/dev/null 2>&1 || true; done
	docker rmi "$recorder" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

echo "building lazycompose"
(cd "$repo" && CGO_ENABLED=0 go build -o "$work/lazycompose" ./cmd/lazycompose)
cp -r "$here/testdata/stacks" "$stacks"

echo "starting the demo stacks"
docker pull -q alpine:3 >/dev/null
docker pull -q docker:cli >/dev/null
for img in $images; do
	if ! docker image inspect "$img" >/dev/null 2>&1; then
		docker tag alpine:3 "$img"
		tagged="$tagged $img"
	fi
done
for d in $dirs; do compose "$d" up -d --quiet-pull >/dev/null 2>&1; done

# quick access: nextcloud pinned, jellyfin and monitoring used lately
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
mkdir -p "$work/state/lazycompose"
cat >"$work/state/lazycompose/history.json" <<EOF
{"uses":{"/srv/stacks/media/jellyfin":["$now","$now","$now"],"/srv/stacks/monitoring/stack":["$now"]},
 "pins":["/srv/stacks/cloud/nextcloud"]}
EOF

screenshot() {
	echo "running lazycompose and waiting for logs"
	tmux kill-session -t "$session" 2>/dev/null || true
	tmux new-session -d -s "$session" -x 132 -y 34 \
		"docker run --rm -it --hostname homeserver -e TERM=xterm-256color -e COLORTERM=truecolor \
		-e XDG_STATE_HOME=/state -v $work/state:/state \
		-v /var/run/docker.sock:/var/run/docker.sock -v $stacks:/srv/stacks \
		-v $work/lazycompose:/usr/local/bin/lazycompose:ro -w /root \
		docker:cli lazycompose -root /srv/stacks -config /dev/null"
	sleep 20
	tmux capture-pane -p -e -t "$session" >"$work/shot.ansi"

	echo "rendering"
	python3 -I "$here/render.py" "$work/shot.ansi" "$work/shot.html"
	chromium --headless=new --no-sandbox --disable-gpu --hide-scrollbars \
		--force-device-scale-factor=2 --default-background-color=00000000 \
		--window-size=1300,900 --screenshot="$work/full.png" "file://$work/shot.html" 2>/dev/null
	magick "$work/full.png" -trim +repage -bordercolor none -border 40 -strip "$repo/docs/screenshot.png"
	echo "wrote docs/screenshot.png"
}

# demo records docs/demo.gif with vhs. Its image gets the docker CLI from
# docker:cli, so lazycompose can reach the demo stacks, and vim for the edit.
demo() {
	echo "recording the demo"
	docker build -q -t "$recorder" - >/dev/null <<EOF
FROM docker:cli AS cli
FROM $vhs
COPY --from=cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=cli /usr/local/libexec/docker/cli-plugins /usr/local/libexec/docker/cli-plugins
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends vim >/dev/null \\
	&& rm -rf /var/lib/apt/lists/* \\
	&& printf 'syntax on\\nset number background=dark laststatus=0 noshowmode\\n' >/root/.vimrc
EOF
	docker run --rm --hostname homeserver --entrypoint sh \
		-e XDG_STATE_HOME=/state -v "$work/state:/state" \
		-v /var/run/docker.sock:/var/run/docker.sock -v "$stacks:/srv/stacks" \
		-v "$work/lazycompose:/usr/local/bin/lazycompose:ro" \
		-v "$here/demo.tape:/demo.tape:ro" -v "$work/out:/out" -w /out \
		"$recorder" -c "vhs -q /demo.tape; s=\$?; chown -R $(id -u):$(id -g) /out /state; exit \$s"
	cp "$work/out/demo.gif" "$repo/docs/demo.gif"
	echo "wrote docs/demo.gif ($(du -h "$repo/docs/demo.gif" | cut -f1))"
}

mkdir -p "$work/out"
case $mode in
png) screenshot ;;
gif) demo ;;
all)
	screenshot
	demo
	;;
esac
