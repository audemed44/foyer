# Installing Foyer

## Docker Compose

```yaml
services:
  foyer:
    image: ghcr.io/audemed44/foyer:latest
    container_name: foyer
    restart: unless-stopped
    user: "1000:1000"
    group_add: ["111"]          # the docker group id: stat -c %g /var/run/docker.sock
    extra_hosts: ["host.docker.internal:host-gateway"]
    environment:
      - TZ=Europe/London
    volumes:
      - ./foyer:/config
      - /var/run/docker.sock:/var/run/docker.sock:ro
      # - ./homepage:/homepage:ro # one-time import from Homepage
    ports:
      - "3030:8080"
```

Open the page and click **Edit** to start. `./foyer` should be owned by the
`user` above; Foyer keeps its config, icons, images, [Drop](drop.md) files
and [alert](alerts.md) state there.

- **The Docker socket** (read-only is enough) powers container state, the
  [containers and logs pages](containers.md), [discovery](dashboard.md#discovery)
  and the [topology map](topology.md). Foyer only reads from Docker; it never
  starts, stops or changes containers.
- **`host.docker.internal`** lets Foyer reach services published by other
  stacks, and processes on the host itself (e.g. Cockpit on `:9090`).

The image is about 20 MB and Foyer idles at around 7 MB of RAM.

## Security

Foyer has **no login**: anyone who can reach it can edit the dashboard, read
container logs and use Drop. Run it on a private network, behind a VPN such
as Tailscale or WireGuard, or behind a reverse proxy that handles
authentication.

Widget credentials never reach the browser (the server fetches widget data
itself), and form posts from other websites are refused.

## HTTPS and the installable app

Serve Foyer over HTTPS, for example as a proxy host in Nginx Proxy Manager,
and your browser can **Install** it (or *Add to Home Screen* on a phone). The
installed app opens instantly from its cached shell and, on Android, appears
in the share sheet: see [Drop](drop.md).

## Environment

| Variable | Default | |
|---|---|---|
| `FOYER_CONFIG_DIR` | `/config` | Holds `foyer.yaml`, `icons/`, `images/`, `drop/` and `alerts.json` |
| `FOYER_PORT` | `8080` | |
| `FOYER_IMPORT_DIR` | `/homepage` | Homepage config to import on first start |
| `FOYER_DOCKER_SOCKET` | `/var/run/docker.sock` | |
| `FOYER_DROP_MAX_MB` | `512` | Largest file Drop accepts |
| `TZ` | UTC | Used for calendar events. The clock uses the browser's time zone. |

Any other variable can be used in `foyer.yaml` as `${NAME}`, which is handy
for widget secrets.

## Moving from Homepage

Mount your Homepage folder (the one holding `config/`, `icons/` and
`images/`) at `/homepage` and start Foyer with an empty `/config`. It
converts `services.yaml`, `settings.yaml`, `widgets.yaml` and
`bookmarks.yaml`, and copies your icons and images across. Once
`foyer.yaml` exists the mount is ignored and can be removed.

To preview the conversion without starting the server:

```sh
docker run --rm -v ./homepage:/homepage:ro ghcr.io/audemed44/foyer import /homepage
```

The Uptime Kuma, Speedtest and calendar (iCal) widgets carry over; other
Homepage widgets are skipped. Homepage's Docker labels keep working for
[discovery](dashboard.md#discovery).
