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
- Widgets for Uptime Kuma, Speedtest Tracker and iCal calendars, plus
  **app widgets**: any app can describe its own card in the
  [Foyer widget format](#app-widgets) (Shelfloom does)
- Customise it from the browser: drag services around, edit groups, change the
  theme (dark OLED, light or system; accent colour; mono, sans or editorial
  type; outline, filled or glass cards; background image), bookmarks and
  custom CSS. Or edit `foyer.yaml` by hand; changes apply without a restart.
- A **Containers** page: every container on the host, grouped by compose
  project, with live CPU, memory, network and health
- **Live logs** for any container (a Dozzle replacement): follow, search,
  stderr highlighting, ANSI colours, timestamps, download
- **Discovery**: in edit mode, running containers that aren't on the
  dashboard are suggested with their name, icon, status check and link filled in
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
    volumes:
      - ./foyer:/config
      - /var/run/docker.sock:/var/run/docker.sock:ro
      # - ./homepage:/homepage:ro # one-time import from Homepage
    ports:
      - "3030:8080"
```

Open the page and click **Edit** to start customising.

Foyer has no login: anyone who can reach it can edit the dashboard and read
container logs. Run it on a private network, behind a VPN, or behind a
reverse proxy that handles authentication.

### Containers, logs and discovery

These need the Docker socket mounted (read-only is enough) and the
container's user in the docker group (`group_add` above). Foyer only reads
from Docker: it never starts, stops or changes containers. Resource stats are
sampled only while a page showing them is open.

A service is linked to its container through `container:`, or automatically
when its status check points at one (`ping: http://sonarr:8989` → `sonarr`).
Linked cards show memory use on hover and a logs button.

Suggestions come from running containers that nothing on the dashboard
points at yet. They can be refined with labels on the container; Homepage's
labels work too, so an existing setup carries over:

| Label | Homepage equivalent | |
|---|---|---|
| `foyer.name` | `homepage.name` | Display name |
| `foyer.group` | `homepage.group` | Group to add it to |
| `foyer.icon` | `homepage.icon` | Icon |
| `foyer.url` | `homepage.href` | Link |
| `foyer.description` | `homepage.description` | |
| `foyer.ping` | `homepage.siteMonitor` | Status check |
| `foyer.hide=true` | | Never suggest this container |

Without labels, Foyer guesses: the name from the compose service, the icon
from the image, the status check from the exposed port, and the link from
the domain your other services share (`https://<name>.example.com`).
Dismissed suggestions are stored under `ignored_containers` in `foyer.yaml`.

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
  accent: "#2563ff"
  font: sans                 # sans (Inter) | mono | serif
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

ignored_containers: [watchtower]   # never suggested in edit mode
```

## App widgets

Apps can serve their own widget, and Foyer renders it in the same style as
the built-in ones. [Shelfloom](https://github.com/audemed44/shelfloom) does
this: books read this year against your goal, streak, reading time, and the
books you're reading as a shelf of covers.

```yaml
      - name: Shelfloom
        url: https://books.example.com
        ping: http://shelfloom:8000
        widget:
          type: app
          url: http://shelfloom:8000/api/foyer/widget
          key: ${SHELFLOOM_TOKEN}   # optional, sent as a bearer token
```

You rarely need to type this: in edit mode, Foyer checks the apps on your
dashboard (and newly discovered containers) for `/api/foyer/widget` and
offers to add the widget.

### The format (version 1)

To give your own app a widget, serve JSON like this (every section is
optional):

```json
{
  "version": 1,
  "stats": [
    { "label": "Read in 2026", "value": "122", "unit": "/52", "caption": "books", "tone": "good" }
  ],
  "progress": [
    { "label": "2026 goal", "value": 122, "max": 52, "caption": "Goal reached" }
  ],
  "items_title": "Currently reading",
  "items_layout": "covers",
  "items": [
    {
      "title": "Dune",
      "subtitle": "Frank Herbert",
      "image": "/api/books/42/cover",
      "url": "/books/42",
      "progress": 64,
      "caption": "64%"
    }
  ]
}
```

- `stats`: up to 6 big figures. `tone` is `good`, `warn`, `bad` or `accent`.
- `progress`: up to 4 labelled bars.
- `items`: up to 12, shown as a row of 2:3 `covers` or a compact `list`.
  `progress` is 0–100.
- `image` paths starting with `/` are fetched from the app by Foyer and
  proxied to the browser, so the app's internal address stays private (only
  same-origin paths; SVG isn't proxied). Public `https://` images are used
  directly.
- `url` paths starting with `/` resolve against the service's link, so they
  open the app's public page.

Foyer refreshes the widget every minute and caches it for 45 seconds.

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
