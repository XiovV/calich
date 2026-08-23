# Calich

An open-source, self-hosted Google Calendar alternative with **CalDAV** sync.

**Current features**

- **Day, Week, Month and Year views**, with drag-to-create, drag-to-move and resize.
- **Recurring events and all-day events**
- **CalDAV**: two-way sync with your phone and other calendar apps.
- **Workspaces**: share calendars with other members, organize them into groups,
  and invite people by email.
- **Attendees and invitations** — Invite others to events.
- **Reminders**: in-app and email.
- **Attachments**
- **Subscribed calendars**: subscribe to any `.ics` URL.
- **Import and export** of `.ics` and `.zip` files.
- **Light and dark themes**, 12/24-hour time, and a configurable week start.

## Screenshots

<details>
<summary><b>Dark mode</b></summary>

<br>

![Week view — dark](docs/screenshots/dark-week.png)
![Month view — dark](docs/screenshots/dark-month.png)
![Event modal — dark](docs/screenshots/dark-event.png)

</details>

<details>
<summary><b>Light mode</b></summary>

<br>

![Week view — light](docs/screenshots/light-week.png)
![Month view — light](docs/screenshots/light-month.png)
![Event modal — light](docs/screenshots/light-event.png)

</details>

## Deployment

Calich is a single binary serving both the API and the web app on one port
(`8080` by default). All persistent state lives under one directory, so a
deployment is the binary (or container) plus a volume.

Reachable from anywhere other than a trusted LAN? Put TLS in front of it and set
`COOKIE_SECURE=true` — see [Behind a reverse proxy](#behind-a-reverse-proxy).

### Docker

The quickest way to get an instance running:

```bash
docker run -d \
  --name calich \
  -p 8080:8080 \
  -v /srv/calich/data:/data \

  -e INITIAL_EMAIL=you@example.com \ # Optional
  -e INITIAL_PASSWORD=change-me \ # Optional
  ghcr.io/xiovv/calich:latest
```

Or with Compose:

```yaml
services:
  calich:
    image: ghcr.io/xiovv/calich:latest
    container_name: calich
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      INITIAL_EMAIL: you@example.com # Optional
      INITIAL_PASSWORD: change-me # Optional
```

```bash
docker compose up -d
```

`INITIAL_EMAIL` and `INITIAL_PASSWORD` create the first account on first start, and
are ignored on every start afterward. They're optional: if you leave them out you
will be redirected to the sign up page automatically to create your account.

To build the image yourself instead of pulling it:

```bash
make docker-build          # or: docker build -t calich-server .
make docker-run            # runs it on :8080, mounting ./data
```

### Pre-compiled binaries

Grab the archive for your platform from the
[Releases page](https://github.com/XiovV/calich/releases), unpack it, and run it:

```bash
tar -xzf calich_linux_amd64.tar.gz
sudo install calich-server /usr/local/bin/

sudo mkdir -p /var/lib/calich
DATA_DIR=/var/lib/calich \
INITIAL_EMAIL=you@example.com \
INITIAL_PASSWORD=change-me \
  calich-server
```

The frontend is embedded in the binary, so that's the whole install. To run it as a
service, a minimal systemd unit:

```ini
# /etc/systemd/system/calich.service
[Unit]
Description=Calich calendar server
After=network.target

[Service]
User=calich
Environment=DATA_DIR=/var/lib/calich
Environment=PORT=8080
ExecStart=/usr/local/bin/calich-server
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now calich
```

### Building from source

You'll need **Go 1.26+**, **Node 22+** and **Yarn**.

```bash
git clone https://github.com/XiovV/calich.git
cd calich

# 1. Build the web app
yarn install --frozen-lockfile
make build-frontend                       # writes ./dist

# 2. Embed it in the server and compile
cp -r dist/. server/internal/static/dist/
make build-backend VERSION=v1.0.0         # writes server/bin/calich-server
```

Step 1 matters: `server/internal/static/dist/` holds a placeholder page in the repo
so the Go build compiles without a frontend present. Skip it and you'll get a
working API serving a blank placeholder instead of the app.

`VERSION` is optional — it's the label shown beside the wordmark and served from
`/api/version`. Left unset, the build honestly reports `dev`.

Then run it:

```bash
DATA_DIR=/var/lib/calich \
INITIAL_EMAIL=you@example.com \
INITIAL_PASSWORD=change-me \
  ./server/bin/calich-server
```

For development, run the two halves separately instead — `make dev-backend` on
`:8080` and `make dev-frontend` for the Vite dev server, which proxies `/api` to it.
`make help` lists every target.

## Configuration

All configuration is by environment variable.

| Variable                                                            | Default             | What it does                                                                                                                                                                     |
| ------------------------------------------------------------------- | ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PORT`                                                              | `8080`              | Port the server listens on.                                                                                                                                                      |
| `DATA_DIR`                                                          | `/data`             | Directory holding all persistent state.                                                                                                                                          |
| `INITIAL_EMAIL`                                                     | —                   | Login identifier for the bootstrap account, created on first start if no account exists.                                                                                         |
| `INITIAL_PASSWORD`                                                  | —                   | Password for that account. Set it and `INITIAL_EMAIL` together, or omit both and register through the first-run form.                                                            |
| `INITIAL_NAME`                                                      | `Admin`             | Display name for that account.                                                                                                                                                   |
| `ENABLE_SIGNUPS`                                                    | `false`             | Allow self-registration. The first account is always creatable regardless.                                                                                                       |
| `COOKIE_SECURE`                                                     | `false`             | Whether the refresh-token cookie is marked `Secure`. Defaults off so a plain-HTTP LAN deployment works out of the box. **Set it to `true` whenever TLS is in front.** See below. |
| `MAX_ATTACHMENT_SIZE`                                               | `26214400` (25 MiB) | Largest single attachment accepted, in bytes.                                                                                                                                    |
| `MAX_ATTACHMENTS_PER_EVENT`                                         | `10`                | How many attachments one event may carry.                                                                                                                                        |
| `SUBSCRIPTION_REFRESH_INTERVAL`                                     | `1h`                | Poll cadence for a subscribed calendar whose feed states none of its own. Go duration syntax.                                                                                    |
| `INVITE_RATE_LIMIT_PER_HOUR`                                        | `100`               | Per-user hourly ceiling on queued invitations.                                                                                                                                   |
| `AUTH_RATE_LIMIT_PER_EMAIL`                                         | `10`                | Failed sign-in attempts per email per 15 minutes, across login and CalDAV.                                                                                                       |
| `AUTH_RATE_LIMIT_PER_IP`                                            | `30`                | Failed sign-in attempts per IP per 15 minutes.                                                                                                                                   |
| `REGISTER_RATE_LIMIT_PER_IP`                                        | `20`                | Registration attempts per IP per hour.                                                                                                                                           |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` | —                   | Outbound mail. Email reminders and invitations are only offered when **all five** are set.                                                                                       |
| `IMAP_HOST` / `IMAP_PORT` / `IMAP_USER` / `IMAP_PASS`               | —                   | The mailbox `SMTP_FROM` sends from, polled for invitation replies. Without it, invitations still send but no response ever comes back.                                           |

### `COOKIE_SECURE`

Leave it `false` if you reach Calich over plain HTTP, such as a LAN address like
`http://192.168.1.50:8080` — browsers discard a `Secure` cookie sent from a
non-HTTPS origin, so switching it on there makes logins silently fail to stick.
Set it to `true` as soon as anything terminates TLS in front of Calich, otherwise
the refresh token can leak over a plaintext request to your host. If Calich sees a
request arrive over TLS while the setting is still off, it logs a warning telling
you to flip it.

### Behind a reverse proxy

Once you're terminating TLS, set `COOKIE_SECURE=true` alongside it:

```yaml
services:
  calich:
    image: ghcr.io/xiovv/calich:latest
    restart: unless-stopped
    volumes:
      - ./data:/data
    environment:
      COOKIE_SECURE: "true" # required once TLS is in front
    expose:
      - "8080"
```

A minimal Caddy site block, which handles certificates automatically:

```caddyfile
calendar.example.com {
    reverse_proxy calich:8080
}
```

Or nginx:

```nginx
server {
    listen 443 ssl;
    server_name calendar.example.com;

    ssl_certificate     /etc/letsencrypt/live/calendar.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/calendar.example.com/privkey.pem;

    location / {
        proxy_pass http://calich:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Keep `X-Forwarded-Proto` in place — it's what lets Calich notice if `COOKIE_SECURE`
is still off and warn you about it.

**Want HTTPS on a LAN address?** A public certificate authority won't issue for a
private IP, but you have two good options: [Tailscale](https://tailscale.com), whose
`tailscale cert` / Serve gives you real HTTPS on a `*.ts.net` name with no cert
management at all; or a real domain resolved to your private IP with a certificate
obtained via the DNS-01 challenge, which Caddy and certbot both support. Either way
you get a trustworthy origin and can set `COOKIE_SECURE=true`.

## Data & backups

Everything this instance keeps is under `DATA_DIR` (default `/data`): `calich.db`
(SQLite) and an `attachments/` directory holding every uploaded file's bytes.

**A backup that copies only `calich.db` silently loses every attachment — back up the
whole `DATA_DIR`, not just the database file.**

## Roadmap

Calendars today come from this instance or from a subscribed `.ics` URL. The next
milestone is mirroring calendars from external providers over their own APIs:

- [ ] **Connections** — authorize and revoke a Google account ([#180](https://github.com/XiovV/calich/issues/180))
- [ ] **Linked calendars** — pick which of a connection's calendars to mirror, and see their events ([#181](https://github.com/XiovV/calich/issues/181))
- [ ] **Full and delta refresh**, with a background poller for connections ([#182](https://github.com/XiovV/calich/issues/182))
- [ ] Linked events carry **RSVP status, conference link and colour** ([#183](https://github.com/XiovV/calich/issues/183))
- [ ] **Sharing linked calendars**, with a viewer-dependent CalDAV home-set ([#184](https://github.com/XiovV/calich/issues/184))
- [ ] Reminders on an external source are **opt-in, off by default** ([#185](https://github.com/XiovV/calich/issues/185))
- [ ] **Connection lifecycle** — disconnect disposition, pending calendars, failure states ([#186](https://github.com/XiovV/calich/issues/186))

Beyond that milestone:

- [ ] **Microsoft** as a second provider — the connection layer is built to be provider-extensible.
- [ ] **Web Push**, layered on the notification records already stored.
- [ ] **Creating** multi-day all-day events from the event modal — they render and import correctly today, but only the API can create one.
- [ ] **Private-address blocking** on subscription URLs, and encrypting stored subscription credentials, before multi-user deployments are recommended.
- [ ] **Reminder catch-up** after downtime — missed fires are currently dropped by design.
