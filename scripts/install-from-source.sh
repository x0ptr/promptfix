#!/usr/bin/env bash
# Build promptfix from this checkout and install it for the current user.
# Linux and macOS only; on Windows use the .exe from `make dist`.
#
# Usage: scripts/install-from-source.sh [--prefix DIR]   (default: ~/.local/bin)
# To install a released binary instead, use install.sh in the repository root.
set -euo pipefail

prefix="${HOME}/.local/bin"
while [[ $# -gt 0 ]]; do
	case "$1" in
	--prefix) prefix="$2"; shift 2 ;;
	-h|--help) sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

case "$(uname -s)" in
Linux|Darwin) ;;
*) echo "unsupported OS $(uname -s); use the binaries from 'make dist'" >&2; exit 1 ;;
esac

command -v go >/dev/null || { echo "go not found (Go 1.27+ required)" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

make build
mkdir -p "$prefix"
install -m 0755 bin/promptfix "$prefix/promptfix"
echo "installed $prefix/promptfix ($("$prefix/promptfix" version))"

case ":$PATH:" in
*":$prefix:"*) ;;
*) echo "note: $prefix is not in your PATH" >&2 ;;
esac
