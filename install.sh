#!/bin/sh
# Install secdrop from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/dumbprism19/secdrop/main/install.sh | sh
#
# Options (environment variables):
#   SECDROP_VERSION       install this version (e.g. 0.5.0) instead of the latest
#   SECDROP_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#
# Every download is checked against the release's checksums.txt. Any failure
# stops the install with an error; nothing is installed half-way.

set -eu

REPO="dumbprism19/secdrop"
# Overridable only so the script can be tested against a local copy of a release.
RELEASES_URL="${SECDROP_RELEASES_URL:-https://github.com/$REPO/releases}"
PROTO="=https" WGET_HTTPS="--https-only"
if [ -n "${SECDROP_RELEASES_URL:-}" ]; then PROTO="=https,http" WGET_HTTPS=""; fi

if [ -t 2 ]; then
	BOLD="$(printf '\033[1m')" RED="$(printf '\033[31m')" YELLOW="$(printf '\033[33m')" GREEN="$(printf '\033[32m')" RESET="$(printf '\033[0m')"
else
	BOLD="" RED="" YELLOW="" GREEN="" RESET=""
fi

say() { printf '%s\n' "$*" >&2; }
warn() { printf '%swarning:%s %s\n' "$YELLOW$BOLD" "$RESET" "$*" >&2; }
fail() {
	printf '%serror:%s %s\n' "$RED$BOLD" "$RESET" "$*" >&2
	say "secdrop was NOT installed."
	exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

# download URL FILE: fails (non-zero) on any HTTP error.
download() {
	if have curl; then
		curl --proto "$PROTO" --tlsv1.2 -fsSL --retry 3 -o "$2" "$1"
	else
		# shellcheck disable=SC2086 # empty or one flag, on purpose
		wget -q $WGET_HTTPS -O "$2" "$1"
	fi
}

# latest_version prints the newest release's version, without a leading "v".
# It follows GitHub's /releases/latest redirect, which avoids API rate limits.
latest_version() {
	if have curl; then
		url="$(curl --proto "$PROTO" --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest")" || return 1
	else
		# shellcheck disable=SC2086 # empty or one flag, on purpose
		url="$(wget -q $WGET_HTTPS -S --spider "$RELEASES_URL/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r')" || return 1
	fi
	tag="${url##*/}"
	case "$tag" in
	v[0-9]*) printf '%s\n' "${tag#v}" ;;
	*) return 1 ;;
	esac
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	elif have openssl; then
		openssl dgst -sha256 -r "$1" | cut -d ' ' -f 1
	else
		return 1
	fi
}

main() {
	have curl || have wget || fail "this installer needs curl or wget."
	have tar || fail "this installer needs tar."

	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "secdrop runs on macOS and Linux only (this is $(uname -s))." ;;
	esac

	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "no secdrop build for this processor ($(uname -m)). Build from source with: go install github.com/$REPO/cmd/secdrop@latest" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple silicon; use the native build.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
		arch=arm64
	fi

	if [ -n "${SECDROP_VERSION:-}" ]; then
		version="${SECDROP_VERSION#v}"
	else
		version="$(latest_version)" || fail "couldn't find the latest secdrop release at $RELEASES_URL/latest (are you online?)."
	fi

	install_dir="${SECDROP_INSTALL_DIR:-$HOME/.local/bin}"
	asset="secdrop_${version}_${os}_${arch}.tar.gz"
	base="$RELEASES_URL/download/v$version"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t secdrop)" || fail "couldn't create a temporary folder."
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Installing ${BOLD}secdrop $version${RESET} ($os/$arch)…"

	download "$base/checksums.txt" "$tmp/checksums.txt" ||
		fail "couldn't download $base/checksums.txt. Does release v$version exist and have files attached?"
	download "$base/$asset" "$tmp/$asset" ||
		fail "couldn't download $base/$asset."

	want="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")"
	[ -n "$want" ] || fail "$asset isn't listed in checksums.txt, so it can't be verified."
	got="$(sha256 "$tmp/$asset")" || fail "need sha256sum, shasum or openssl to verify the download."
	[ "$want" = "$got" ] || fail "checksum mismatch for $asset (expected $want, got $got). The download may be corrupted or tampered with."

	tar -xzf "$tmp/$asset" -C "$tmp" secdrop || fail "couldn't unpack $asset."
	[ -f "$tmp/secdrop" ] || fail "$asset doesn't contain a secdrop binary."
	chmod 755 "$tmp/secdrop"

	mkdir -p "$install_dir" || fail "couldn't create $install_dir. Set SECDROP_INSTALL_DIR to a folder you can write to."
	[ -w "$install_dir" ] || fail "can't write to $install_dir. Set SECDROP_INSTALL_DIR to a folder you can write to."
	# Move into place in one step, so a running secdrop is never half-overwritten.
	cp "$tmp/secdrop" "$install_dir/.secdrop.new" || fail "couldn't write to $install_dir."
	mv -f "$install_dir/.secdrop.new" "$install_dir/secdrop" || fail "couldn't replace $install_dir/secdrop."

	installed="$("$install_dir/secdrop" --version 2>&1)" || fail "installed $install_dir/secdrop, but it doesn't run: $installed"

	say "${GREEN}${BOLD}✓${RESET} Installed $installed to $install_dir/secdrop"

	case ":$PATH:" in
	*":$install_dir:"*)
		found="$(command -v secdrop 2>/dev/null || true)"
		if [ -n "$found" ] && [ "$found" != "$install_dir/secdrop" ]; then
			warn "another secdrop at $found comes first in your PATH, so typing secdrop will run that one."
			say "  Remove it (e.g. rm \"$found\") or put $install_dir earlier in your PATH."
		fi
		;;
	*)
		# Shown to the user as typed, so the ~ stays literal.
		# shellcheck disable=SC2088
		case "${SHELL:-}" in
		*/zsh) rc="~/.zshrc" ;;
		*/bash) if [ "$os" = darwin ]; then rc="~/.bash_profile"; else rc="~/.bashrc"; fi ;;
		*/fish) rc="" ;;
		*) rc="~/.profile" ;;
		esac
		say ""
		warn "$install_dir isn't in your PATH yet. Add it with:"
		if [ -z "$rc" ]; then
			say "  fish_add_path $install_dir"
		else
			say "  echo 'export PATH=\"$install_dir:\$PATH\"' >> $rc && . $rc"
		fi
		;;
	esac

	if ! have gcloud; then
		say ""
		warn "secdrop signs in through the gcloud CLI, which isn't installed: https://cloud.google.com/sdk/docs/install"
	fi

	say ""
	say "Run ${BOLD}secdrop${RESET} to get started. It updates itself when a new release is out."
}

main "$@"
