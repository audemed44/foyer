# ── Frontend ────────────────────────────────────────────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ── Server ──────────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=frontend /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /foyer ./cmd/foyer \
    && mkdir -p /config

# ── Runtime: a static binary and nothing else ───────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /foyer /foyer
COPY --from=server --chown=nonroot:nonroot /config /config
ENV FOYER_CONFIG_DIR=/config \
    FOYER_PORT=8080 \
    GOMEMLIMIT=32MiB
EXPOSE 8080
VOLUME ["/config"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/foyer", "healthcheck"]
ENTRYPOINT ["/foyer"]
