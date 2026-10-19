#!/usr/bin/env bash
# Build for the server's CPU and install under /opt/waifuwebshelf there.
# Usage: scripts/deploy.sh user@host
set -euo pipefail
cd "$(dirname "$0")/.."

host=${1:?Usage: scripts/deploy.sh user@host}

cpu=$(ssh "$host" uname -m)
case "$cpu" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  armv7l | armv6l) arch=arm ;;
  *)
    echo "Unsupported server CPU: $cpu" >&2
    exit 1
    ;;
esac

make dist ARCH="$arch"

# The tarball waits in a private folder with a random name, so no other
# account on the server can put its own tarball in front of root.
tmp=$(ssh "$host" mktemp -d)
remote=$(printf %q "$tmp/waifuwebshelf.tar.gz")
# Paths are quoted with %q and meant to expand here, before they are sent.
# shellcheck disable=SC2029
cleanup() {
  # The file is missing when the copy failed before writing it.
  ssh "$host" "if [ -e $remote ]; then rm -- $remote; fi; rmdir -- $(printf %q "$tmp")"
}
trap cleanup EXIT

scp -q "dist/waifuwebshelf-linux-$arch.tar.gz" "$host:$tmp/waifuwebshelf.tar.gz"
ssh -t "$host" "sudo install -d -m 755 /opt/waifuwebshelf \
  && sudo tar -xzf $remote -C /opt/waifuwebshelf --no-same-owner bin \
  && sudo /opt/waifuwebshelf/bin/install.sh $remote"
