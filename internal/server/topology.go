package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/topology"
	"github.com/audemed44/foyer/internal/widgets"
)

// integration fetches the data of the first service with a widget of the
// given type (through the widget cache). svc is nil when there's none.
func (s *Server) integration(ctx context.Context, cfg config.Config, kind string) (svc *config.Service, data any, err error) {
	kinds := []string{kind}
	if kind == "proxy" {
		kinds = []string{"gatehouse", "npm"} // Gatehouse wins if both are set up
	}
	for _, k := range kinds {
		for _, candidate := range cfg.Services() {
			if candidate.Widget != nil && candidate.Widget.Type() == k {
				data, err = s.widgets.Fetch(ctx, candidate.ID, candidate.Widget)
				return candidate, data, err
			}
		}
	}
	return nil, nil, nil
}

// containerOf is the container a tool's service runs in: its container
// field, the host of its status check or widget URL, or else the one
// running the tool's image.
func containerOf(svc *config.Service, kind string, list []docker.Container) string {
	if svc == nil {
		return ""
	}
	names := map[string]bool{}
	for _, c := range list {
		names[c.Name] = true
	}
	if svc.Container != "" && names[svc.Container] {
		return svc.Container
	}
	for _, raw := range []string{svc.Ping, svc.Widget.String("url")} {
		if u, err := url.Parse(config.ExpandEnv(raw)); err == nil && names[u.Hostname()] {
			return u.Hostname()
		}
	}
	for _, c := range list {
		if strings.Contains(strings.ToLower(c.Image), kind) {
			return c.Name
		}
	}
	return ""
}

func errText(err error) *string {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &msg
}

func (s *Server) getTopology(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := s.store.Config()
	in := topology.Input{Config: cfg, Details: map[string]docker.Details{}}

	var wg sync.WaitGroup
	var npmSvc, kopiaSvc, syncSvc *config.Service
	var npmData, kopiaData, syncData any
	var npmErr, kopiaErr, syncErr error
	wg.Add(3)
	go func() { defer wg.Done(); npmSvc, npmData, npmErr = s.integration(ctx, cfg, "proxy") }()
	go func() { defer wg.Done(); kopiaSvc, kopiaData, kopiaErr = s.integration(ctx, cfg, "kopia") }()
	go func() { defer wg.Done(); syncSvc, syncData, syncErr = s.integration(ctx, cfg, "syncthing") }()

	if s.docker != nil {
		list, err := s.docker.List(ctx)
		in.Sources.Docker = errText(err)
		if err == nil {
			in.Containers = list
			var mu sync.Mutex
			var dwg sync.WaitGroup
			for _, c := range list {
				dwg.Add(1)
				go func() {
					defer dwg.Done()
					if d, err := s.docker.Inspect(ctx, c); err == nil {
						mu.Lock()
						in.Details[c.ID] = d
						mu.Unlock()
					}
				}()
			}
			dwg.Wait()
		}
	}
	wg.Wait()

	if npmSvc != nil {
		in.Sources.NPM = errText(npmErr)
		if d, ok := widgets.AsProxy(npmData); ok {
			in.NPM = &d
		}
	}
	if kopiaSvc != nil {
		in.Sources.Kopia = errText(kopiaErr)
		if d, ok := kopiaData.(widgets.KopiaData); ok {
			in.Kopia = &d
			in.KopiaContainer = containerOf(kopiaSvc, "kopia", in.Containers)
		}
	}
	if syncSvc != nil {
		in.Sources.Syncthing = errText(syncErr)
		if d, ok := syncData.(widgets.SyncthingData); ok {
			in.Syncthing = &d
			in.SyncthingContainer = containerOf(syncSvc, "syncthing", in.Containers)
		}
	}
	writeJSON(w, http.StatusOK, topology.Build(in))
}

// npmLinks maps container names to the public URL the reverse proxy
// (Gatehouse or NPM) serves them at, for discovery. It's empty without a
// proxy widget.
func (s *Server) npmLinks(ctx context.Context, cfg config.Config, list []docker.Container) map[string]string {
	links := map[string]string{}
	_, data, err := s.integration(ctx, cfg, "proxy")
	npm, ok := widgets.AsProxy(data)
	if err != nil || !ok {
		return links
	}
	for _, h := range npm.Hosts {
		if len(h.Domains) == 0 || !h.Enabled || strings.HasPrefix(h.Domains[0], "*") {
			continue
		}
		name, _ := topology.Resolve(h.ForwardHost, h.ForwardPort, list, nil)
		if _, taken := links[name]; name == "" || taken {
			continue
		}
		scheme := "http"
		if h.SSL {
			scheme = "https"
		}
		links[name] = scheme + "://" + h.Domains[0]
	}
	return links
}
