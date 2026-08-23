# WaifuWebShelf

A personal catalog of manga, light novels, and merch. One Go binary, SQLite, server-rendered HTML. Public pages are a shelf; adding and editing uses a login form at `/login`.

A **series** is a franchise. Manga vs light novels are **book lines** under that series, each with its own owned/missing volume grid. Merch hangs off the series.

## Run locally

```bash
go test ./...
ADMIN_PASS=changeme go run ./cmd/waifuwebshelf
```

Then open http://localhost:8080 and log in at http://localhost:8080/login (user `admin`, password from `ADMIN_PASS`).

Run from the repository root so `templates/` and `static/` resolve. The SQLite file and uploads live in `data/` (gitignored).

### Environment

| Variable | Default | Notes |
| --- | --- | --- |
| `PORT` | `8080` | Listen port |
| `DATA_DIR` | `data` | SQLite + `images/` |
| `ADMIN_USER` | `admin` | Login username |
| `ADMIN_PASS` | empty | If unset, `/admin` and `/login` return 503 |

Money is stored as integer minor units (cents for CAD/USD, yen for JPY). Leave MSRP blank when you do not know list price; spending totals skip those rows instead of treating them as $0.

## Deploy on a VPS

Build a binary, copy `templates/` and `static/` next to it, and point `DATA_DIR` at a writable directory.

```bash
go build -o waifuwebshelf ./cmd/waifuwebshelf
```

### systemd

`/etc/systemd/system/waifuwebshelf.service`:

```ini
[Unit]
Description=WaifuWebShelf
After=network.target

[Service]
Type=simple
User=waifuwebshelf
WorkingDirectory=/opt/waifuwebshelf
ExecStart=/opt/waifuwebshelf/waifuwebshelf
Environment=PORT=8080
Environment=DATA_DIR=/var/lib/waifuwebshelf
Environment=ADMIN_USER=admin
Environment=ADMIN_PASS=set-a-real-password
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`WorkingDirectory` must contain `templates/` and `static/`. Create `/var/lib/waifuwebshelf` for the database and photos.

### Caddy

```caddy
shelf.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

### nginx

```nginx
server {
    listen 443 ssl;
    server_name shelf.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 10m;
    }
}
```

Health check: `GET /healthz`.
