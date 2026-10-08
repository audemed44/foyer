package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/alerts"
	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/widgets"
)

// integrationEvery spaces out the Kopia, Syncthing and NPM checks; their
// problems (stale backups, expiring certificates) move slowly.
const integrationEvery = 5 * time.Minute

// CheckAlerts runs after each round of status checks. It does nothing
// unless alerts have somewhere to go.
func (s *Server) CheckAlerts(ctx context.Context) {
	cfg := s.store.Config()
	a := cfg.Alerts
	if a.AppriseURL == "" {
		return
	}
	var off []string
	for prefix, on := range map[string]bool{
		"service:": a.Services, "container:": a.Containers, "backup:": a.Backups,
		"sync:": a.Sync, "cert:": a.Certificates,
	} {
		if !on {
			off = append(off, prefix)
		}
	}
	s.alerts.Forget(off...)

	var scope []string
	var problems []alerts.Problem
	if a.Services {
		scope = append(scope, "service:")
		problems = append(problems, s.serviceProblems(ctx, cfg)...)
	}
	if a.Containers {
		scope = append(scope, "container:")
		problems = append(problems, s.containerProblems(ctx, cfg)...)
	}
	s.alerts.Evaluate(ctx, a, scope, problems)

	if time.Since(s.lastIntegrations) < integrationEvery {
		return
	}
	s.lastIntegrations = time.Now()
	scope, problems = nil, nil
	add := func(on bool, prefix, kind string, check func(any) []alerts.Problem) {
		if !on {
			return
		}
		svc, data, err := s.integration(ctx, cfg, kind)
		if svc == nil {
			return
		}
		scope = append(scope, prefix, "reach:"+kind)
		if err != nil {
			problems = append(problems, alerts.Problem{
				Key: "reach:" + kind, Title: "Can't reach " + svc.Name, Body: err.Error(),
				Level: "warning", After: 2, Recovered: svc.Name + " is reachable again",
			})
			return
		}
		problems = append(problems, check(data)...)
	}
	add(a.Backups, "backup:", "backup", backupProblems)
	add(a.Sync, "sync:", "syncthing", syncProblems)
	add(a.Certificates, "cert:", "proxy", certProblems)
	s.alerts.Evaluate(ctx, a, scope, problems)
}

// serviceProblems leaves out services Lookout watches (it alerts on them
// itself) and apps Gatehouse put to sleep.
func (s *Server) serviceProblems(ctx context.Context, cfg config.Config) []alerts.Problem {
	var out []alerts.Problem
	status := s.serviceStatus(ctx)
	down := max(1, cfg.Alerts.DownAfter)
	for _, svc := range cfg.Services() {
		st := status[svc.ID]
		if st.Check != nil || (st.Container != nil && st.Container.State == "asleep") {
			continue
		}
		p := alerts.Problem{Key: "service:" + svc.ID, Level: "failure", After: down,
			Recovered: svc.Name + " is back up"}
		switch {
		case st.Ping != nil && st.Ping.State == "down":
			p.Title = svc.Name + " is down"
			p.Body = st.Ping.Error
			if p.Body == "" {
				p.Body = fmt.Sprintf("It answered HTTP %d.", st.Ping.Code)
			}
		case st.Container != nil && st.Container.State != "running":
			p.Title = svc.Name + " is down"
			p.Body = fmt.Sprintf("Its container %s is %s.", st.Container.Name, st.Container.State)
		case st.Container != nil && st.Container.Health == "unhealthy":
			p.Title, p.Level = svc.Name+" is unhealthy", "warning"
			p.Body = fmt.Sprintf("Docker reports %s as unhealthy.", st.Container.Name)
			p.Recovered = svc.Name + " is healthy again"
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

var exitCode = regexp.MustCompile(`^Exited \((\d+)\)`)

// containerProblems covers every container, except those a dashboard
// service already reports on.
func (s *Server) containerProblems(ctx context.Context, cfg config.Config) []alerts.Problem {
	if s.docker == nil {
		return nil
	}
	list, err := s.docker.List(ctx)
	if err != nil {
		return nil
	}
	covered := map[string]bool{}
	for name := range s.asleepContainers(ctx, cfg) {
		covered[name] = true // stopped on purpose by Gatehouse
	}
	for _, st := range s.monitor.Status() {
		if st.Container != nil {
			covered[st.Container.Name] = true
		}
	}
	var out []alerts.Problem
	for _, c := range list {
		if covered[c.Name] {
			continue
		}
		p := alerts.Problem{Key: "container:" + c.Name, Level: "failure", After: max(1, cfg.Alerts.DownAfter),
			Recovered: c.Name + " is running normally again"}
		m := exitCode.FindStringSubmatch(c.Status)
		switch {
		case c.State == "restarting":
			p.Title, p.Body = c.Name+" keeps restarting", c.Status
		case c.State == "exited" && m != nil && m[1] != "0":
			p.Title, p.Body = fmt.Sprintf("%s stopped with exit code %s", c.Name, m[1]), c.Status
		case c.Health == "unhealthy":
			p.Title, p.Level, p.Body = c.Name+" is unhealthy", "warning", c.Status
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

func backupProblems(data any) []alerts.Problem {
	d, ok := widgets.AsBackup(data)
	if !ok {
		return nil
	}
	tool := "Kopia has no snapshot of it."
	if d.Engine == "keep" {
		tool = "Keep has no good backup of it."
	}
	var out []alerts.Problem
	for _, src := range d.Sources {
		name := src.Label
		p := alerts.Problem{Key: "backup:" + src.Key, Level: "failure",
			Recovered: "Backups of " + name + " are running again"}
		switch src.State {
		case "stale":
			p.Title = "The backup of " + name + " is overdue"
			if src.Last != nil {
				p.Body = "The last good backup finished " + time.Since(*src.Last).Round(time.Hour).String() + " ago."
			}
		case "never":
			p.Title, p.Body = name+" has never been backed up", tool
		case "errors":
			p.Title, p.Level = "The backup of "+name+" had problems", "warning"
			if src.Errors > 0 {
				p.Body = fmt.Sprintf("%d files failed in the last snapshot.", src.Errors)
			} else {
				p.Body = "The last backup failed or warned; see " + backupTool(d.Engine) + "."
			}
			p.Recovered = "The backup of " + name + " is complete again"
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

func backupTool(engine string) string {
	if engine == "keep" {
		return "Keep's run log"
	}
	return "Kopia"
}

func syncProblems(data any) []alerts.Problem {
	d, ok := data.(widgets.SyncthingData)
	if !ok {
		return nil
	}
	var out []alerts.Problem
	for _, f := range d.Folders {
		if f.State != "error" && f.PullErrors == 0 {
			continue
		}
		body := f.Error
		if body == "" {
			body = fmt.Sprintf("%d files couldn't sync.", f.PullErrors)
		}
		out = append(out, alerts.Problem{
			Key: "sync:" + f.ID, Title: "Syncthing folder " + f.Label + " has errors", Body: body,
			Level: "failure", Recovered: "Syncthing folder " + f.Label + " is syncing again",
		})
	}
	return out
}

func certProblems(data any) []alerts.Problem {
	d, ok := widgets.AsProxy(data)
	if !ok {
		return nil
	}
	var out []alerts.Problem
	for _, c := range d.Certificates {
		if c.Expires.IsZero() || c.Days >= d.WarnDays || c.Hosts == 0 {
			continue
		}
		p := alerts.Problem{Key: "cert:" + c.Name, Level: "warning",
			Title:     fmt.Sprintf("The certificate for %s expires in %d days", c.Name, c.Days),
			Body:      "Domains: " + strings.Join(c.Domains, ", "),
			Recovered: "The certificate for " + c.Name + " was renewed"}
		if c.Days < 0 {
			p.Level, p.Title = "failure", "The certificate for "+c.Name+" has expired"
		}
		out = append(out, p)
	}
	return out
}

type alertsResponse struct {
	Enabled bool           `json:"enabled"`
	Open    []alerts.Event `json:"open"`
	History []alerts.Event `json:"history"`
}

func (s *Server) getAlerts(w http.ResponseWriter, _ *http.Request) {
	openNow, history := s.alerts.Snapshot()
	writeJSON(w, http.StatusOK, alertsResponse{
		Enabled: s.store.Config().Alerts.AppriseURL != "", Open: openNow, History: history,
	})
}

// testAlert sends a test notification, to the settings in the body (so the
// editor can test before saving) or else the saved ones.
func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "expected JSON")
		return
	}
	a := s.store.Config().Alerts
	var body config.Alerts
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err == nil && body.AppriseURL != "" {
		a.AppriseURL, a.Tag = body.AppriseURL, body.Tag
	}
	if a.AppriseURL == "" {
		writeError(w, http.StatusBadRequest, "set an Apprise URL first")
		return
	}
	err := alerts.Send(r.Context(), a, alerts.Event{
		Level: "info", Title: "Foyer test notification", Body: "Alerts from Foyer will arrive here.",
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Sent"})
}
