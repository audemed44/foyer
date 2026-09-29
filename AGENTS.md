# Foyer

Instructions for coding agents working in this repository. `CLAUDE.md`
imports this file.

## Project

A self-hosted homelab dashboard (a lighter, nicer replacement for
gethomepage.dev). A Go server (`cmd/foyer`, `internal/`) serves a JSON API
and the Preact + TypeScript frontend (`frontend/`), which is built into
`web/dist` and embedded in the binary. The config is a YAML file
(`/config/foyer.yaml`) that can be edited by hand or from the UI.

## Constraints

- **Low memory is a feature.** The server idles under ~10 MB RSS and the
  image is ~18 MB (distroless static). Don't add heavy dependencies or
  long-lived caches; prefer the standard library. Host stats are read
  straight from /proc and /sys (Linux only).
- The Go module has one dependency (yaml.v3). Justify any new one.
- Docker access (`internal/docker`) is read-only: list, inspect, stats, logs.
  `Inspect` decodes only mounts and networks, never the environment. Stats
  are sampled on demand and cached for a few seconds, never polled in the
  background. Container names from requests are resolved against the list
  (`Client.Find`) before reaching the Docker API.
- There is no authentication by design; Foyer runs on private networks.
  Endpoints that accept plain form posts (Drop, the `/share` target) refuse
  `Sec-Fetch-Site: cross-site` so other websites can't post to them.
- Drop (`internal/drop`) stores items in `/config/drop/items.json` and files
  in `/config/drop/files/`, streamed to disk. Uploaded files are served as
  downloads unless they're a passive type (images, video, audio, PDF, text).
- Widget settings (URLs, API keys) never reach the browser: `Config.Public()`
  strips them and widget data is fetched server-side. Secret fields are
  masked for the editor and restored on save (`RestoreSecrets`).
- UI style: Swiss editorial on black — heavy Inter headlines with tight
  tracking, tracked uppercase eyebrows, 2px rules over numbered headings,
  hairline frames, square corners, one accent (#2563ff). Everything is themed
  through CSS variables in `frontend/src/styles.css`. Check phone width too.

## Commits

Conventional Commits: `<type>(<scope>): <summary>`, e.g. `feat(widgets): ...`.

## Checks before pushing

Go is not installed on the dev host; run it in Docker if needed:

```sh
go vet ./... && go test ./...            # needs web/dist to exist (npm run build)
cd frontend && npm run format:check && npm run typecheck && npm test && npm run build
docker build -t foyer:dev .
```
