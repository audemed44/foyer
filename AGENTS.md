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
- Widget settings (URLs, API keys) never reach the browser: `Config.Public()`
  strips them and widget data is fetched server-side. Secret fields are
  masked for the editor and restored on save (`RestoreSecrets`).
- UI style: Swiss / OLED — black, hairline borders, square corners, one
  accent colour, Geist Mono. Everything is themed through CSS variables in
  `frontend/src/styles.css`. Check phone width too.

## Commits

Conventional Commits: `<type>(<scope>): <summary>`, e.g. `feat(widgets): ...`.

## Checks before pushing

Go is not installed on the dev host; run it in Docker if needed:

```sh
go vet ./... && go test ./...            # needs web/dist to exist (npm run build)
cd frontend && npm run format:check && npm run typecheck && npm test && npm run build
docker build -t foyer:dev .
```
