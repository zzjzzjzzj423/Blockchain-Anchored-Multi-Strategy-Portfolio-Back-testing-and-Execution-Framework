#!/bin/sh
set -eu

go_version="1.27.4"
install_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/.tools"
go_binary="$install_root/go/bin/go"

if [ -x "$go_binary" ]; then
	installed_version="$($go_binary version)"
	case "$installed_version" in
		*"go${go_version}"*)
			echo "Go ${go_version} is already installed in .tools/go"
			exit 0
			;;
		*)
			echo "A different local Go toolchain already exists: $installed_version" >&2
			echo "Remove .tools/go explicitly before changing the pinned version." >&2
			exit 1
			;;
	esac
fi

os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch_name="$(uname -m)"
case "$arch_name" in
	arm64|aarch64) arch_name="arm64" ;;
	x86_64|amd64) arch_name="amd64" ;;
	*) echo "Unsupported architecture: $arch_name" >&2; exit 1 ;;
esac

case "${os_name}-${arch_name}" in
	darwin-arm64) expected_sha256="91a44a2b8a716600ad94a4f143e156116620db8828086a6e3602291be9bd0bb4" ;;
	darwin-amd64) expected_sha256="2b799377704ef12db39bd254995d5c348c9cc8d86562ca817724390e56c38f43" ;;
	linux-arm64) expected_sha256="3392582988a273a87ab5043ee1b2c9c17a2c3c00580bf9326c47a2e0aaaeac92" ;;
	linux-amd64) expected_sha256="7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840" ;;
	*) echo "Unsupported platform: ${os_name}-${arch_name}" >&2; exit 1 ;;
esac

archive_name="go${go_version}.${os_name}-${arch_name}.tar.gz"
download_url="https://go.dev/dl/${archive_name}"
task_tmp_dir="$(mktemp -d)"
trap 'rm -rf "$task_tmp_dir"' EXIT HUP INT TERM

echo "Downloading ${download_url}"
curl --fail --location --proto '=https' --tlsv1.2 --output "$task_tmp_dir/$archive_name" "$download_url"

actual_sha256="$(shasum -a 256 "$task_tmp_dir/$archive_name" | awk '{print $1}')"
if [ "$actual_sha256" != "$expected_sha256" ]; then
	echo "Go archive checksum mismatch" >&2
	echo "expected: $expected_sha256" >&2
	echo "actual:   $actual_sha256" >&2
	exit 1
fi

tar -C "$task_tmp_dir" -xzf "$task_tmp_dir/$archive_name"
mkdir -p "$install_root"
mv "$task_tmp_dir/go" "$install_root/go"
"$go_binary" version
