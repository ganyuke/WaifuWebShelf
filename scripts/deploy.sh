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
scp -q "dist/waifuwebshelf-linux-$arch.tar.gz" "$host:/tmp/waifuwebshelf.tar.gz"
ssh -t "$host" 'sudo install -d -m 755 /opt/waifuwebshelf \
  && sudo tar -xzf /tmp/waifuwebshelf.tar.gz -C /opt/waifuwebshelf --no-same-owner bin \
  && sudo /opt/waifuwebshelf/bin/install.sh /tmp/waifuwebshelf.tar.gz'
