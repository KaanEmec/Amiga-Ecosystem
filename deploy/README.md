# VPS preparation

Nothing here deploys anything. It prepares the artefacts for one Linux VPS with one
application instance behind an HTTPS reverse proxy.

## Build the Linux binary

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/amiga ./cmd/amiga
```

The binary is static (pure-Go SQLite); use `GOARCH=arm64` for ARM hosts.

## Install layout

| Path | Contents |
|---|---|
| `/opt/amiga/amiga` | the binary |
| `/opt/amiga/data/amiga-data.json`, `/opt/amiga/images/` | seed content, read once when the database is empty |
| `/var/lib/amiga/` | runtime data: `amiga.db`, `media/`, `staging/` (owned by the `amiga` user, mode 750) |
| `/etc/amiga/amiga.env` | secrets and configuration, from `amiga.env.example`, mode 600 |

```bash
useradd --system --home /var/lib/amiga --shell /usr/sbin/nologin amiga
install -d -o amiga -g amiga -m 750 /var/lib/amiga
cp deploy/amiga.service /etc/systemd/system/ && systemctl daemon-reload && systemctl enable --now amiga
```

## Owner account

Run once on the server as the service user, with the same environment file:

```bash
sudo -u amiga env $(cat /etc/amiga/amiga.env | xargs) /opt/amiga/amiga owner init
```

Store the printed TOTP secret in an authenticator app and keep the recovery codes offline.
`owner reset-password`, `owner reset-totp`, `owner recovery-codes` and `owner unlock` (clears
failed sign-in attempts when the login throttle is locking you out) are the local recovery commands.

## Reverse proxy

`Caddyfile` (recommended, automatic HTTPS) or `nginx.conf.example`. The app listens on
localhost only and relies on the proxy for TLS and compression. Turnstile keys must be
issued for the real domain and `AMIGA_BASE_URL` must match it exactly. Keep
`AMIGA_TRUST_PROXY=1` so rate limits and the login throttle see real client addresses
instead of the proxy's loopback address; the app warns at startup if it is missing.

## Health and backups

`GET /healthz` returns `{"status":"ok"}` after a database check; point the VPS monitor at it.
`backup.sh <dir>` writes a consistent SQLite copy (`VACUUM INTO`) plus the media directory and
keeps the last 14. Restore by stopping the service, replacing `/var/lib/amiga/amiga.db` and
`media/`, and starting it again. `amiga export` / `amiga import --replace` move content as JSON.
