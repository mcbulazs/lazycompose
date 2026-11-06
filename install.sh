#!/bin/sh
# Installs the latest lazycompose release:
#
#   curl -fsSL https://raw.githubusercontent.com/mcbulazs/lazycompose/main/install.sh | sh
#
# Options go after "sh -s --", for example
#
#   curl -fsSL .../install.sh | sh -s -- -b ~/.local/bin
#
#   -b DIR      install into DIR (default /usr/local/bin, with sudo if needed)
#   -v VERSION  install this version, like v0.4.0 (default the latest)
#
# It downloads the release archive for this machine, checks it against the
# release's checksums.txt and installs the binary. Later updates are
# "lazycompose -update".
set -eu

repo=mcbulazs/lazycompose
tmp=

die() {
	echo "lazycompose install: $*" >&2
	exit 1
}

usage() {
	cat <<EOF
usage: install.sh [-b DIR] [-v VERSION]

  -b DIR      install into DIR (default /usr/local/bin, with sudo if needed)
  -v VERSION  install this version, like v0.4.0 (default the latest)
EOF
}

fetch() { # fetch URL FILE
	if command -v curl >/dev/null 2>&1; then
		curl --proto '=https' --tlsv1.2 -fsSL -o "$2" "$1" || die "could not download $1"
	elif command -v wget >/dev/null 2>&1; then
		https=
		# busybox wget has no --https-only; the URLs are https anyway
		if wget --help 2>&1 | grep -q -- --https-only; then https=--https-only; fi
		wget $https -q -O "$2" "$1" || die "could not download $1"
	else
		die "needs curl or wget"
	fi
}

sha256() { # sha256 FILE
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "needs sha256sum or shasum to check the download"
	fi
}

main() {
	dir=/usr/local/bin
	version=latest
	while [ $# -gt 0 ]; do
		case $1 in
		-b)
			[ $# -ge 2 ] || die "-b needs a folder"
			dir=$2
			shift 2
			;;
		-v)
			[ $# -ge 2 ] || die "-v needs a version"
			version=$2
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option $1, see -h" ;;
		esac
	done

	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	MINGW* | MSYS* | CYGWIN*) die "on Windows, download lazycompose_windows_amd64.zip from https://github.com/$repo/releases/latest" ;;
	*) die "there are release binaries for Linux, macOS and Windows only, not $(uname -s); use go install, see the README" ;;
	esac
	m=$(uname -m)
	# a shell running under Rosetta on Apple silicon says x86_64
	if [ "$os" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
		m=arm64
	fi
	case $m in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "there is no release binary for $m, only x86_64 and arm64; use go install, see the README" ;;
	esac
	case $version in
	latest) base=https://github.com/$repo/releases/latest/download ;;
	v[0-9]*.[0-9]*.[0-9]*) base=https://github.com/$repo/releases/download/$version ;;
	*) die "the version should look like v0.4.0, not $version" ;;
	esac

	asset=lazycompose_${os}_$arch.tar.gz
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' INT TERM

	echo "downloading $asset ($version)"
	fetch "$base/$asset" "$tmp/$asset"
	fetch "$base/checksums.txt" "$tmp/checksums.txt"
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")
	[ -n "$want" ] || die "checksums.txt has no entry for $asset"
	[ "$(sha256 "$tmp/$asset")" = "$want" ] || die "$asset does not match checksums.txt, not installing it"
	tar -xzf "$tmp/$asset" -C "$tmp" lazycompose || die "could not unpack $asset"

	sudo=
	if [ -d "$dir" ] && [ -w "$dir" ]; then
		:
	elif [ ! -e "$dir" ] && mkdir -p "$dir" 2>/dev/null; then
		:
	elif [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
		echo "$dir is not writable, using sudo"
		sudo=sudo
	else
		die "cannot write to $dir; run as root or pick another folder with -b"
	fi
	$sudo mkdir -p "$dir"
	$sudo install -m 755 "$tmp/lazycompose" "$dir/lazycompose"

	echo "installed $("$dir/lazycompose" -version) at $dir/lazycompose"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "note: $dir is not in your PATH" ;;
	esac
	found=$(command -v lazycompose 2>/dev/null || true)
	if [ -n "$found" ] && [ "$found" != "$dir/lazycompose" ]; then
		echo "note: $found comes first in your PATH, so that one runs"
	fi
	echo "to update later, run: lazycompose -update"
}

main "$@"
