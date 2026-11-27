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
	TTY=1
	BOLD="$(printf '\033[1m')" RED="$(printf '\033[31m')" YELLOW="$(printf '\033[33m')" GREEN="$(printf '\033[32m')" RESET="$(printf '\033[0m')"
else
	TTY=""
	BOLD="" RED="" YELLOW="" GREEN="" RESET=""
fi

say() { printf '%s\n' "$*" >&2; }
warn() { printf '%swarning:%s %s\n' "$YELLOW$BOLD" "$RESET" "$*" >&2; }
fail() {
	if [ -n "$STEP" ]; then step_end bad; fi
	# Errors from a quiet download were held back so they don't break the step line.
	if [ -s "${tmp:-}/download.err" ]; then sed 's/^/  /' "$tmp/download.err" >&2; fi
	printf '%serror:%s %s\n' "$RED$BOLD" "$RESET" "$*" >&2
	say "secdrop was NOT installed."
	exit 1
}

# Steps show as "  Label…" while running, then turn into "  ✓ Label" (or ✗ on
# failure, via fail). On a terminal the line is rewritten in place; otherwise
# only the finished line is printed. With a progress bar (step_begin LABEL bar)
# the bar is drawn on the line below the label and cleared when the step ends.
STEP="" STEP_BAR=""
step_begin() {
	STEP="$1" STEP_BAR=""
	if [ "${2:-}" = bar ] && [ -n "$BAR_OK" ]; then STEP_BAR=1; fi
	[ -n "$TTY" ] || return 0
	if [ -n "$STEP_BAR" ]; then printf '  %s…\n' "$STEP" >&2; else printf '  %s…' "$STEP" >&2; fi
}
step_end() {
	if [ "$1" = ok ]; then mark="${GREEN}${BOLD}✓${RESET}"; else mark="${RED}${BOLD}✗${RESET}"; fi
	if [ -n "$TTY" ] && [ -n "$STEP_BAR" ] && [ "$1" = ok ]; then
		# The cursor sits below the finished bar: clear it, then rewrite the label line.
		printf '\033[1A\033[2K\033[1A' >&2
	fi
	# On failure mid-bar, curl/wget's own error stays on screen and the ✗ goes below it.
	if [ -n "$TTY" ]; then printf '\r\033[2K' >&2; fi
	printf '  %s %s\n' "$mark" "$STEP" >&2
	STEP="" STEP_BAR=""
}
step_ok() { step_end ok; }

have() { command -v "$1" >/dev/null 2>&1; }

# BAR_OK is set when a download progress bar can be drawn.
BAR_OK=""
if [ -n "$TTY" ] && { have curl || wget --help 2>&1 | grep -q -- --show-progress; }; then BAR_OK=1; fi

# download URL FILE [bar]: fails (non-zero) on any HTTP error. With "bar" and
# a terminal, shows a progress bar (wget needs --show-progress, 1.16+);
# otherwise it is silent and any error is saved for fail to print.
download() {
	rm -f "$tmp/download.err"
	if [ "${3:-}" = bar ] && [ -n "$BAR_OK" ]; then
		if have curl; then
			curl --proto "$PROTO" --tlsv1.2 -fSL --progress-bar --retry 3 -o "$2" "$1"
		else
			# shellcheck disable=SC2086 # empty or one flag, on purpose
			wget -q --show-progress $WGET_HTTPS -O "$2" "$1"
		fi
	elif have curl; then
		curl --proto "$PROTO" --tlsv1.2 -fsSL --retry 3 -o "$2" "$1" 2>"$tmp/download.err"
	else
		# shellcheck disable=SC2086 # empty or one flag, on purpose
		wget -q $WGET_HTTPS -O "$2" "$1" 2>"$tmp/download.err"
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

	step_begin "Downloading checksums"
	download "$base/checksums.txt" "$tmp/checksums.txt" ||
		fail "couldn't download $base/checksums.txt. Does release v$version exist and have files attached?"
	step_ok

	step_begin "Downloading $asset" bar
	download "$base/$asset" "$tmp/$asset" bar ||
		fail "couldn't download $base/$asset."
	step_ok

	step_begin "Verifying checksum"
	want="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")"
	[ -n "$want" ] || fail "$asset isn't listed in checksums.txt, so it can't be verified."
	got="$(sha256 "$tmp/$asset")" || fail "need sha256sum, shasum or openssl to verify the download."
	[ "$want" = "$got" ] || fail "checksum mismatch for $asset (expected $want, got $got). The download may be corrupted or tampered with."
	step_ok

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
