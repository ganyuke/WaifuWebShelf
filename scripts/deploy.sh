#!/usr/bin/env bash
# Copy a tarball from `make dist` to a server and install it under /opt/waifuwebshelf.
# Usage: scripts/deploy.sh user@host [dist/waifuwebshelf-linux-amd64.tar.gz]
set -euo pipefail

host=${1:?Usage: scripts/deploy.sh user@host [tarball]}
tarball=${2:-dist/waifuwebshelf-linux-amd64.tar.gz}
if [[ ! -f "$tarball" ]]; then
  echo "$tarball not found. Run make dist first." >&2
  exit 1
fi

tmp=$(ssh "$host" mktemp -d)
scp -q "$tarball" "$host:$tmp/waifuwebshelf.tar.gz"
ssh -t "$host" "sudo install -d -m 755 /opt/waifuwebshelf \
  && sudo tar -xzf $tmp/waifuwebshelf.tar.gz -C /opt/waifuwebshelf --no-same-owner bin \
  && sudo /opt/waifuwebshelf/bin/install.sh $tmp/waifuwebshelf.tar.gz; s=\$?; rm -rf $tmp; exit \$s"
