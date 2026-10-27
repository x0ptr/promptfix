#!/usr/bin/env bash
# promptfix installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/x0ptr/promptfix/main/install.sh | bash
#
# Environment:
#   PROMPTFIX_VERSION        release tag to install, e.g. v0.1.0 (default: latest)
#   PROMPTFIX_INSTALL_DIR    target directory (default: ~/.local/bin)
#   PROMPTFIX_DOWNLOAD_URL   base URL serving the release assets (tests, mirrors)
#   PROMPTFIX_VERIFY_ATTESTATION=1
#                            also verify the build provenance with `gh attestation verify`
#
# Windows: use install.ps1.

# Everything runs inside main, called on the last line, so a truncated
# download never executes half a script.
main() {
	set -euo pipefail

	local repo="x0ptr/promptfix"
	local version="${PROMPTFIX_VERSION:-}"
	local dir="${PROMPTFIX_INSTALL_DIR:-$HOME/.local/bin}"

	need tar
	need uname
	local fetch sha
	fetch="$(pick_fetch)"
	sha="$(pick_sha)"

	local os arch
	os="$(detect_os)"
	arch="$(detect_arch "$os")"
	local asset="promptfix_${os}_${arch}.tar.gz"

	local base
	if [[ -n "${PROMPTFIX_DOWNLOAD_URL:-}" ]]; then
		base="${PROMPTFIX_DOWNLOAD_URL%/}"
	elif [[ -n "$version" ]]; then
		[[ "$version" == v* ]] || die "PROMPTFIX_VERSION must be a tag like v0.1.0, got '$version'"
		base="https://github.com/$repo/releases/download/$version"
	else
		base="https://github.com/$repo/releases/latest/download"
	fi

	local tmp
	tmp="$(mktemp -d)"
	# shellcheck disable=SC2064 # expand $tmp now
	trap "rm -rf '$tmp'" EXIT

	say "downloading $asset ${version:-(latest)}"
	download "$fetch" "$base/$asset" "$tmp/$asset" ||
		die "download failed: $base/$asset${version:+ (does release $version exist?)}"
	download "$fetch" "$base/checksums.txt" "$tmp/checksums.txt" ||
		die "download failed: $base/checksums.txt"

	local want got
	want="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")"
	[[ -n "$want" ]] || die "no checksum for $asset in checksums.txt"
	got="$($sha "$tmp/$asset" | awk '{ print $1 }')"
	[[ "$want" == "$got" ]] || die "checksum mismatch for $asset (expected $want, got $got) – nothing installed"
	say "checksum ok"

	if [[ "${PROMPTFIX_VERIFY_ATTESTATION:-}" == 1 ]]; then
		need gh
		gh attestation verify "$tmp/$asset" --repo "$repo" >&2 ||
			die "attestation verification failed – nothing installed"
	fi

	tar -xzf "$tmp/$asset" -C "$tmp" promptfix || die "could not extract $asset"

	local old=""
	if [[ -x "$dir/promptfix" ]]; then
		old="$("$dir/promptfix" version 2>/dev/null | awk '{ print $2 }' || true)"
	fi
	mkdir -p "$dir"
	# Install atomically: copy next to the target, then rename.
	cp "$tmp/promptfix" "$dir/.promptfix.tmp.$$"
	chmod 0755 "$dir/.promptfix.tmp.$$"
	mv -f "$dir/.promptfix.tmp.$$" "$dir/promptfix"

	local new
	new="$("$dir/promptfix" version | awk '{ print $2 }')"
	if [[ -n "$old" && "$old" != "$new" ]]; then
		say "updated $dir/promptfix: $old -> $new"
	else
		say "installed $dir/promptfix $new"
	fi

	case ":$PATH:" in
	*":$dir:"*) ;;
	*)
		say "note: $dir is not in your PATH; add it, e.g.:"
		say "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.$(basename "${SHELL:-bash}")rc"
		;;
	esac
}

say() { printf 'promptfix: %s\n' "$*" >&2; }

die() {
	say "error: $*"
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"
}

pick_fetch() {
	if command -v curl >/dev/null 2>&1; then
		echo curl
	elif command -v wget >/dev/null 2>&1; then
		echo wget
	else
		die "'curl' or 'wget' is required"
	fi
}

pick_sha() {
	if command -v sha256sum >/dev/null 2>&1; then
		echo sha256sum
	elif command -v shasum >/dev/null 2>&1; then
		echo "shasum -a 256"
	else
		die "'sha256sum' or 'shasum' is required"
	fi
}

download() {
	local tool="$1" url="$2" out="$3"
	if [[ "$tool" == curl ]]; then
		curl -fsSL -o "$out" "$url"
	else
		wget -q -O "$out" "$url"
	fi
}

unsupported() {
	die "unsupported platform $1 – supported: linux and macOS on amd64/arm64 (Windows: install.ps1)"
}

detect_os() {
	local s
	s="$(uname -s)"
	case "$s" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) unsupported "$s" ;;
	esac
}

detect_arch() {
	local m
	m="$(uname -m)"
	case "$m" in
	x86_64 | amd64)
		# A shell under Rosetta reports x86_64 on Apple silicon.
		if [[ "$1" == darwin && "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" == 1 ]]; then
			echo arm64
		else
			echo amd64
		fi
		;;
	aarch64 | arm64) echo arm64 ;;
	*) unsupported "$(uname -s)/$m" ;;
	esac
}

main "$@"
