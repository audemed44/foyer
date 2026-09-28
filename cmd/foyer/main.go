// Command foyer serves the dashboard.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image has no zoneinfo; TZ needs this

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/monitor"
	"github.com/audemed44/foyer/internal/server"
	"github.com/audemed44/foyer/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) > 2 && os.Args[1] == "import" {
		os.Exit(importCmd(os.Args[2]))
	}

	level := slog.LevelInfo
	if os.Getenv("FOYER_DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	configDir := env("FOYER_CONFIG_DIR", "/config")
	store := config.NewStore(filepath.Join(configDir, "foyer.yaml"))
	if err := firstRun(store, configDir); err != nil {
		slog.Error("could not create the config", "err", err)
		os.Exit(1)
	}
	if _, err := store.Get(); err != nil {
		slog.Warn("config has errors; serving defaults until it's fixed", "err", err)
	}

	socket := env("FOYER_DOCKER_SOCKET", "/var/run/docker.sock")
	if _, err := os.Stat(socket); err != nil {
		slog.Info("docker socket not found; container state disabled", "path", socket)
		socket = ""
	}
	var dock *docker.Client
	if socket != "" {
		dock = docker.New(socket)
	}
	mon := monitor.New(store, dock, env("FOYER_PROC", "/proc"), env("FOYER_SYS", "/sys"))

	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	srv := &http.Server{
		Addr:              ":" + env("FOYER_PORT", "8080"),
		Handler:           server.New(store, mon, dock, configDir, dist).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go mon.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("foyer listening", "addr", srv.Addr, "config", store.Path())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// firstRun creates foyer.yaml when it's missing, importing a gethomepage.dev
// config if one is mounted at FOYER_IMPORT_DIR (default /homepage).
func firstRun(store *config.Store, configDir string) error {
	if _, err := os.Stat(store.Path()); err == nil {
		return nil
	}
	if dir, ok := config.FindHomepage(env("FOYER_IMPORT_DIR", "/homepage")); ok {
		cfg, err := config.ImportHomepage(dir)
		if err != nil {
			slog.Warn("homepage import failed; starting from defaults", "err", err)
		} else {
			config.CopyAssets(dir, configDir)
			slog.Info("imported homepage config", "from", dir, "groups", len(cfg.Groups))
			return store.WriteInitial(cfg)
		}
	}
	cfg := config.Default()
	cfg.Groups = []config.Group{{
		Name: "Getting started",
		Services: []config.Service{{
			Name: "Foyer", URL: "https://github.com/audemed44/foyer",
			Description: "Docs and config reference", Icon: "github.png",
		}},
	}}
	return store.WriteInitial(cfg)
}

// importCmd prints a homepage config converted to Foyer's format.
func importCmd(dir string) int {
	homepage, ok := config.FindHomepage(dir)
	if !ok {
		fmt.Fprintf(os.Stderr, "no services.yaml in %s or %s/config\n", dir, dir)
		return 1
	}
	cfg, err := config.ImportHomepage(homepage)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	data, err := config.Marshal(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_, _ = os.Stdout.Write(data)
	return 0
}

func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + env("FOYER_PORT", "8080") + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 1
	}
	return 0
}
