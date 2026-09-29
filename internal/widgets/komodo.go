package widgets

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type KomodoStack struct {
	Name     string `json:"name"`
	State    string `json:"state"` // running | stopped | down | unhealthy | deploying | … | unknown
	Server   string `json:"server,omitempty"`
	Services int    `json:"services"`
	// Updates counts services whose image has a newer version available.
	Updates int `json:"updates"`
}

type KomodoUpdate struct {
	Operation string    `json:"operation"` // e.g. "Deploy Stack"
	Target    string    `json:"target,omitempty"`
	Success   bool      `json:"success"`
	At        time.Time `json:"at"`
	User      string    `json:"user,omitempty"`
}

type KomodoData struct {
	Stacks  []KomodoStack  `json:"stacks"`
	Running int            `json:"running"`
	Updates int            `json:"updates"`
	Servers []KomodoServer `json:"servers"`
	Recent  []KomodoUpdate `json:"recent"`
}

type KomodoServer struct {
	Name  string `json:"name"`
	State string `json:"state"` // Ok | NotOk | Disabled
}

// komodo reads stacks, servers and recent activity from Komodo Core.
// Settings: url, key, secret (an API key; a service user with read access
// is enough).
func komodo(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "key", "secret"); err != nil {
		return nil, err
	}
	base := w.String("url")
	auth := []string{"X-Api-Key", w.String("key"), "X-Api-Secret", w.String("secret")}
	read := func(request string, params, out any) error {
		return postJSON(ctx, join(base, "/read/"+request), params, out, auth...)
	}

	var stacks []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Info struct {
			State      string `json:"state"`
			ServerName string `json:"server_name"`
			Services   []struct {
				Service         string `json:"service"`
				UpdateAvailable bool   `json:"update_available"`
			} `json:"services"`
		} `json:"info"`
	}
	if err := read("ListStacks", map[string]any{}, &stacks); err != nil {
		return nil, err
	}
	var servers []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Info struct {
			State string `json:"state"`
		} `json:"info"`
	}
	if err := read("ListServers", map[string]any{}, &servers); err != nil {
		return nil, err
	}
	var updates struct {
		Updates []struct {
			Operation string `json:"operation"`
			StartTS   int64  `json:"start_ts"`
			Success   bool   `json:"success"`
			Username  string `json:"username"`
			Target    struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"target"`
		} `json:"updates"`
	}
	// Activity is a nice extra; older Komodo versions shape it differently.
	_ = read("ListUpdates", map[string]any{"query": map[string]any{}, "page": 0}, &updates)

	names := map[string]string{}
	out := KomodoData{Stacks: []KomodoStack{}, Servers: []KomodoServer{}, Recent: []KomodoUpdate{}}
	for _, s := range stacks {
		names[s.ID] = s.Name
		st := KomodoStack{Name: s.Name, State: s.Info.State, Server: s.Info.ServerName, Services: len(s.Info.Services)}
		for _, svc := range s.Info.Services {
			if svc.UpdateAvailable {
				st.Updates++
			}
		}
		if st.State == "running" {
			out.Running++
		}
		out.Updates += st.Updates
		out.Stacks = append(out.Stacks, st)
	}
	for _, s := range servers {
		names[s.ID] = s.Name
		out.Servers = append(out.Servers, KomodoServer{Name: s.Name, State: s.Info.State})
	}
	for _, u := range updates.Updates {
		// Deploys, pulls, restarts and the like; not config edits or system jobs.
		if !komodoActivity(u.Operation) {
			continue
		}
		out.Recent = append(out.Recent, KomodoUpdate{
			Operation: spaceCamel(u.Operation), Target: names[u.Target.ID], Success: u.Success,
			At: time.UnixMilli(u.StartTS), User: u.Username,
		})
		if len(out.Recent) == 5 {
			break
		}
	}
	sort.SliceStable(out.Stacks, func(i, j int) bool {
		ri, rj := komodoRank(out.Stacks[i]), komodoRank(out.Stacks[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out.Stacks[i].Name) < strings.ToLower(out.Stacks[j].Name)
	})
	return out, nil
}

// komodoRank puts problems first, then stacks with updates, then the rest.
func komodoRank(s KomodoStack) int {
	switch s.State {
	case "unhealthy", "down", "restarting":
		return 0
	case "running":
		if s.Updates > 0 {
			return 1
		}
		return 2
	}
	return 3
}

func komodoActivity(op string) bool {
	for _, verb := range []string{"Deploy", "Pull", "Restart", "Start", "Stop", "Destroy", "Build", "Run"} {
		if strings.HasPrefix(op, verb) {
			return true
		}
	}
	return false
}

// spaceCamel turns "DeployStack" into "Deploy Stack".
func spaceCamel(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
