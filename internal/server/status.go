package server

import (
	"context"
	"math"
	"net/url"
	"strings"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/monitor"
	"github.com/audemed44/foyer/internal/widgets"
)

// serviceStatus is the monitor's view of each service, with two corrections
// from other apps when they're set up:
//
//   - Lookout is the monitor: a service it watches takes its status from
//     the matching check (an HTTP check on the service's address, or a
//     Docker check on its container), and Foyer doesn't ping it.
//   - Gatehouse stops idle apps on purpose (scale-to-zero): their stopped
//     containers show as asleep rather than exited.
func (s *Server) serviceStatus(ctx context.Context) map[string]monitor.ServiceStatus {
	cfg := s.store.Config()
	status := s.monitor.Status()

	asleep := s.asleepContainers(ctx, cfg)
	for id, st := range status {
		if c := st.Container; c != nil && c.State != "running" {
			if state, ok := asleep[c.Name]; ok {
				copied := *c
				copied.State, copied.Status = "asleep", "Asleep ("+state+" by Gatehouse)"
				st.Container = &copied
				status[id] = st
			}
		}
	}

	for id, check := range s.lookoutChecks(ctx, cfg) {
		st := status[id]
		st.Check = &monitor.Check{Name: check.Name, State: check.Status, Message: check.Message,
			LatencyMS: int64(math.Round(check.LatencyMS))}
		st.Ping = nil // Lookout's check replaces Foyer's own
		status[id] = st
	}
	return status
}

// asleepContainers are the containers Gatehouse stopped on purpose, with
// their state (sleeping, waking or stopping).
func (s *Server) asleepContainers(ctx context.Context, cfg config.Config) map[string]string {
	out := map[string]string{}
	_, data, err := s.integration(ctx, cfg, "proxy")
	p, ok := widgets.AsProxy(data)
	if err != nil || !ok {
		return out
	}
	for _, h := range p.Hosts {
		if h.Container != "" && h.State != "" && h.State != "awake" {
			out[h.Container] = h.State
		}
	}
	return out
}

// lookoutChecks maps service IDs to the Lookout check watching them.
func (s *Server) lookoutChecks(ctx context.Context, cfg config.Config) map[string]widgets.LookoutCheck {
	_, data, err := s.integration(ctx, cfg, "lookout")
	d, ok := widgets.AsLookout(data)
	if err != nil || !ok {
		return nil
	}
	out := map[string]widgets.LookoutCheck{}
	for _, svc := range cfg.Services() {
		if c, ok := matchCheck(svc, d.Checks); ok {
			out[svc.ID] = c
		}
	}
	return out
}

// watchedByLookout lists the services Lookout watches, so the monitor
// doesn't ping them too.
func (s *Server) WatchedByLookout(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for id := range s.lookoutChecks(ctx, s.store.Config()) {
		out[id] = true
	}
	return out
}

// matchCheck finds the check for a service: an HTTP check on the host of
// its status URL or link first, then a Docker check on its container.
func matchCheck(svc *config.Service, checks []widgets.LookoutCheck) (widgets.LookoutCheck, bool) {
	hosts := map[string]bool{}
	for _, raw := range []string{svc.Ping, svc.URL} {
		if h := hostOf(config.ExpandEnv(raw)); h != "" {
			hosts[h] = true
		}
	}
	for _, c := range checks {
		if c.Type == "http" && hosts[hostOf(c.Target)] {
			return c, true
		}
	}
	if svc.Container != "" {
		for _, c := range checks {
			if c.Type == "docker" && c.Target == svc.Container {
				return c, true
			}
		}
	}
	return widgets.LookoutCheck{}, false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
