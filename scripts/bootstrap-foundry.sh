#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
install_dir="$project_root/.tools/foundry-v1.9.0"
archive_url="https://github.com/foundry-rs/foundry/releases/download/v1.9.0/foundry_v1.9.0_darwin_arm64.tar.gz"
archive_sha256="38f9af72b266c38ce2e4dfc659ae9cbd1f78c01b34738a76b6c4cb9610579426"

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
    echo "bootstrap-foundry.sh currently pins the darwin_arm64 artifact" >&2
    exit 1
fi
if [ -x "$install_dir/bin/forge" ]; then
    "$install_dir/bin/forge" --version
    exit 0
fi

temporary_dir=$(mktemp -d)
archive="$temporary_dir/foundry.tar.gz"
curl --http1.1 --retry 5 --retry-all-errors -fsSL -o "$archive" "$archive_url"
actual_sha256=$(shasum -a 256 "$archive" | awk '{print $1}')
if [ "$actual_sha256" != "$archive_sha256" ]; then
    echo "Foundry archive checksum mismatch" >&2
    exit 1
fi
mkdir -p "$install_dir/bin"
tar -xzf "$archive" -C "$install_dir/bin"
"$install_dir/bin/forge" --version
