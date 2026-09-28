# Foyer

A small, good-looking start page for your homelab. It's a lighter replacement
for [Homepage](https://gethomepage.dev): one static Go binary serving a
Preact frontend, **~7 MB of RAM** at idle, in an 18 MB image.

- Services grouped into sections, with live up/down checks and Docker
  container state
- Host stats in the header (CPU and memory sparklines, disks, temperature,
  uptime)
- One search box for everything: type to filter your services and press Enter
  to open one, or search the web when nothing matches. Press `/` to focus it.
- Widgets for Uptime Kuma, Speedtest Tracker and iCal calendars
- Customise it from the browser: drag services around, edit groups, change the
  theme (dark OLED, light or system; accent colour; mono, sans or editorial
  type; outline, filled or glass cards; background image), bookmarks and
  custom CSS. Or edit `foyer.yaml` by hand; changes apply without a restart.
- Imports an existing Homepage config on first start

## Run it

```yaml
services:
  foyer:
    image: ghcr.io/audemed44/foyer:latest
    restart: unless-stopped
    user: "1000:1000"
    group_add: ["111"]          # the docker group id, to read container state
    extra_hosts: ["host.docker.internal:host-gateway"]
    environment:
      - TZ=Europe/London
      - FOYER_PASSWORD=change-me  # enables edit mode
    volumes:
      - ./foyer:/config
      - /var/run/docker.sock:/var/run/docker.sock:ro
      # - ./homepage:/homepage:ro # one-time import from Homepage
    ports:
      - "3030:8080"
```

Open the page and click **Edit** in the bottom-right corner to start
customising. Without `FOYER_PASSWORD`, the dashboard is read-only and is
configured through the YAML file.

### Moving from Homepage

Mount your Homepage folder (the one holding `config/`, `icons/` and
`images/`) at `/homepage` and start Foyer with an empty `/config`. It
converts `services.yaml`, `settings.yaml`, `widgets.yaml` and
`bookmarks.yaml`, and copies your icons and images across. Once
`foyer.yaml` exists, the mount is ignored and can be removed.

To preview the conversion without starting the server:

```sh
docker run --rm -v ./homepage:/homepage:ro ghcr.io/audemed44/foyer import /homepage
```

Supported widgets are carried over (`uptimekuma`, `speedtest`, and
`calendar` with an iCal integration); others are skipped.

## Environment

| Variable | Default | |
|---|---|---|
| `FOYER_PASSWORD` | — | Password for edit mode. Unset means read-only. |
| `FOYER_CONFIG_DIR` | `/config` | Holds `foyer.yaml`, `icons/` and `images/`. |
| `FOYER_PORT` | `8080` | |
| `FOYER_IMPORT_DIR` | `/homepage` | Homepage config to import on first start. |
| `FOYER_DOCKER_SOCKET` | `/var/run/docker.sock` | |
| `TZ` | UTC | Used for calendar events. The clock uses the browser's time zone. |

## Config

`/config/foyer.yaml`. Every key is optional; the UI writes the full file.

```yaml
title: Home
open_in_new_tab: true
ping_interval: 30            # seconds between status checks

theme:
  mode: dark                 # dark | light | auto
  accent: "#6b8fff"
  font: mono                 # mono | sans | serif
  cards: outline             # outline | filled | glass
  density: comfortable       # comfortable | compact
  columns: 4                 # max columns on wide screens
  background: /images/bg.jpg # URL or a file in /config/images
  background_dim: 0.75
  background_blur: 0
  custom_css: ""

header:
  greeting: true
  name: Sam
  clock: true
  clock_24h: true
  search:
    enabled: true
    provider: google         # google | duckduckgo | bing | kagi | custom
    url: ""                  # for custom: the query is appended
  system:
    enabled: true
    cpu: true
    memory: true
    temperature: true
    uptime: true
    disks: [/]               # paths inside the container

groups:
  - name: Media
    columns: 2               # optional; by default a group sizes to its services
    collapsed: false
    services:
      - name: Jellyfin
        url: https://jellyfin.example.com
        description: Movies & TV
        icon: jellyfin.svg
        ping: http://jellyfin:8096     # checked from the server; <500 means up
        container: jellyfin            # shows running / unhealthy / stopped

      - name: Speedtest
        url: https://speed.example.com
        icon: speedtest-tracker.png
        widget:
          type: speedtest
          url: http://speedtest-tracker:80
          key: ${SPEEDTEST_TOKEN}      # ${VAR} reads from the environment
          span: 2                      # grid columns (widgets default to 2)

      - name: Uptime Kuma
        widget: { type: uptimekuma, url: "http://uptime-kuma:3001", slug: main }

      - name: Calendar
        widget: { type: calendar, url: "https://example.com/cal.ics", days: 30, max_events: 8 }

bookmarks:
  - name: Dev
    links:
      - { name: GitHub, url: https://github.com, abbr: GH }
```

**Icons** can be a [dashboard-icons](https://github.com/homarr-labs/dashboard-icons)
name (`sonarr.png`, `jellyfin.svg`), a Simple Icons slug (`si-github`), a
URL, or a path such as `/icons/app.png` for files in `/config/icons`.

**Secrets.** Widget settings never reach the browser; the server fetches
widget data itself. In edit mode, keys and tokens are masked and kept
unless you type a new value. You can also keep them out of the file with
`${ENV_VAR}` references.

## Development

```sh
cd frontend && npm install && npm run dev   # Vite on :5173, proxies /api to :8080
go run ./cmd/foyer                          # needs FOYER_CONFIG_DIR=./.data
```

`npm run build` writes to `web/dist`, which is embedded into the binary.
See `AGENTS.md` for the checks CI runs.
