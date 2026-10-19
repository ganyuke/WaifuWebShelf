#!/usr/bin/env bash
# Install or update WaifuWebShelf under /opt/waifuwebshelf from a tarball
# built with `make dist`. scripts/deploy.sh copies it over and runs this.
#
# Usage: sudo /opt/waifuwebshelf/bin/install.sh waifuwebshelf-linux-amd64.tar.gz
set -euo pipefail

ROOT=/opt/waifuwebshelf
APP_USER=waifuwebshelf
tarball=${1:?Usage: install.sh TARBALL}

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root: sudo $0 $tarball" >&2
  exit 1
fi

echo "==> Creating layout under $ROOT"
if ! id "$APP_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$ROOT" --shell /usr/sbin/nologin "$APP_USER"
fi
install -d -m 755 "$ROOT" "$ROOT/bin"
install -d -m 700 -o "$APP_USER" -g "$APP_USER" "$ROOT/data"

# Each deploy gets its own folder under releases/ and app points at the
# newest one. Older ones stay for going back: sudo ln -sfn releases/OLD /opt/waifuwebshelf/app
release=releases/$(date +%Y%m%d-%H%M%S)
echo "==> Unpacking $tarball into $ROOT/$release"
install -d -m 755 "$ROOT/releases" "$ROOT/$release"
tar -xzf "$tarball" -C "$ROOT/$release" --strip-components=1 --no-same-owner app
ln -sfn "$release" "$ROOT/app.next"
mv -T "$ROOT/app.next" "$ROOT/app"

# Keep the newest five releases. Only real folders named like a release are
# removed, never the one app points at.
current=$(readlink "$ROOT/app")
mapfile -t old < <(
  find "$ROOT/releases" -mindepth 1 -maxdepth 1 -type d -regextype posix-extended \
    -regex '.*/[0-9]{8}-[0-9]{6}' -printf '%f\n' | sort
)
for ((i = 0; i < ${#old[@]} - 5; i++)); do
  name=${old[i]}
  [[ releases/$name == "$current" ]] && continue
  echo "==> Removing old release $name"
  rm -r --one-file-system -- "$ROOT/releases/$name"
done

if [[ ! -f "$ROOT/.env" ]]; then
  echo "==> Creating $ROOT/.env"
  install -m 600 "$ROOT/app/env.example" "$ROOT/.env"
fi

echo "==> Installing systemd unit"
install -m 644 "$ROOT/app/waifuwebshelf.service" /etc/systemd/system/waifuwebshelf.service
systemctl daemon-reload
systemctl enable --quiet waifuwebshelf
first=1
[[ -f "$ROOT/data/shelf.db" ]] && first=0
systemctl restart waifuwebshelf

sleep 2
if ! systemctl is-active --quiet waifuwebshelf; then
  journalctl -u waifuwebshelf -n 20 --no-pager
  echo "The service didn't start. The log is above." >&2
  exit 1
fi

echo
echo "Deploy complete."
if [[ "$first" -eq 1 ]]; then
  echo
  echo "Next steps:"
  if [[ -f "$ROOT/data/admin-password" ]]; then
    echo "  1. Log in at /login as admin with password $(cat "$ROOT/data/admin-password")"
  else
    echo "  1. Log in at /login as admin with ADMIN_PASS from $ROOT/.env"
  fi
  echo "  2. Set SITE_URL in $ROOT/.env, then: sudo systemctl restart waifuwebshelf"
  echo "  3. Point Caddy at 127.0.0.1:8080:"
  echo "       shelf.example.com {"
  echo "           reverse_proxy 127.0.0.1:8080"
  echo "       }"
fi
