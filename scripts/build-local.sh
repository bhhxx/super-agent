#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
install_dir="${SUPER_AGENT_INSTALL_DIR:-/usr/local/bin}"
binary_name="${SUPER_AGENT_BINARY_NAME:-super-agent}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

go build -trimpath -ldflags="-s -w" -o "$tmp_dir/$binary_name" "$repo_root"

if [[ -w "$install_dir" ]]; then
	install -m 0755 "$tmp_dir/$binary_name" "$install_dir/$binary_name"
elif command -v sudo >/dev/null 2>&1; then
	sudo install -m 0755 "$tmp_dir/$binary_name" "$install_dir/$binary_name"
else
	printf 'cannot write %s; rerun with sudo or set SUPER_AGENT_INSTALL_DIR\n' "$install_dir" >&2
	exit 1
fi

printf 'installed %s\n' "$install_dir/$binary_name"
