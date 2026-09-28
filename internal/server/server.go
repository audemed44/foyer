// Package server exposes the JSON API and serves the built frontend.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/monitor"
	"github.com/audemed44/foyer/internal/widgets"
)

type Server struct {
	store     *config.Store
	monitor   *monitor.Monitor
	docker    *docker.Client // nil without a Docker socket
	widgets   *widgets.Service
	assetsDir string // holds icons/ and images/
	web       fs.FS
}

func New(store *config.Store, mon *monitor.Monitor, dock *docker.Client, assetsDir string, web fs.FS) *Server {
	return &Server{
		store: store, monitor: mon, widgets: widgets.NewService(), docker: dock,
		assetsDir: assetsDir, web: web,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("GET /api/config/edit", s.getEditableConfig)
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/system", s.getSystem)
	mux.HandleFunc("GET /api/widgets/{id}", s.getWidget)
	mux.HandleFunc("GET /api/widgets/{id}/image", s.getWidgetImage)
	mux.HandleFunc("GET /api/icons", s.listIcons)
	mux.HandleFunc("GET /api/containers", s.listContainers)
	mux.HandleFunc("GET /api/discover", s.discoverServices)
	mux.HandleFunc("GET /api/containers/{name}/logs", s.containerLogs)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("GET /icons/", assetHandler(filepath.Join(s.assetsDir, "icons"), "/icons/"))
	mux.Handle("GET /images/", assetHandler(filepath.Join(s.assetsDir, "images"), "/images/"))
	mux.Handle("GET /", s.spa())
	return securityHeaders(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type configResponse struct {
	Config      config.Config `json:"config"`
	Error       string        `json:"error,omitempty"`
	WidgetTypes []string      `json:"widget_types"`
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.store.Get()
	resp := configResponse{
		Config:      cfg.Public(),
		WidgetTypes: widgets.Types(),
	}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getEditableConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Config().Masked())
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "expected JSON")
		return
	}
	cfg := config.Default()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid config: "+err.Error())
		return
	}
	saved, err := s.store.Save(cfg)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.monitor.Refresh()
	writeJSON(w, http.StatusOK, saved.Masked())
}

func (s *Server) getStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.monitor.Status())
}

func (s *Server) getSystem(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.monitor.System())
}

func (s *Server) getWidget(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Config()
	svc := cfg.Service(r.PathValue("id"))
	if svc == nil || svc.Widget == nil {
		writeError(w, http.StatusNotFound, "no such widget")
		return
	}
	data, err := s.widgets.Fetch(r.Context(), svc.ID, svc.Widget)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, widgets.ErrUnknownType) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) listIcons(w http.ResponseWriter, _ *http.Request) {
	icons := []string{}
	entries, _ := os.ReadDir(filepath.Join(s.assetsDir, "icons"))
	for _, e := range entries {
		if !e.IsDir() {
			icons = append(icons, "/icons/"+e.Name())
		}
	}
	sort.Strings(icons)
	writeJSON(w, http.StatusOK, icons)
}

func assetHandler(dir, prefix string) http.Handler {
	files := http.StripPrefix(prefix, http.FileServer(noListing{http.Dir(dir)}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// noListing hides directory indexes.
type noListing struct{ fs http.FileSystem }

func (n noListing) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err == nil && info.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// spa serves the built frontend, falling back to index.html for app routes.
func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(s.web, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.web, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) getWidgetImage(w http.ResponseWriter, r *http.Request) {
	svc := s.store.Config().Service(r.PathValue("id"))
	if svc == nil || svc.Widget == nil {
		writeError(w, http.StatusNotFound, "no such widget")
		return
	}
	if err := widgets.ProxyImage(r.Context(), svc.Widget, r.URL.Query().Get("path"), w); err != nil {
		// Headers may already be sent if the copy failed midway; then this is a no-op.
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
