package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/discover"
	"github.com/audemed44/foyer/internal/monitor"
	"github.com/audemed44/foyer/internal/topology"
)

// withNPM serves a dashboard whose only service is an NPM widget pointing at
// a fake NPM that forwards web.example.com to the "web" container.
func withNPM(t *testing.T) http.Handler {
	npm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens":
			fmt.Fprint(w, `{"token":"t","expires":"2099-01-01T00:00:00Z"}`)
		case "/api/nginx/proxy-hosts":
			fmt.Fprint(w, `[{"domain_names":["web.example.com"],"forward_scheme":"http","forward_host":"web","forward_port":80,"enabled":true,"certificate_id":1},
				{"domain_names":["gone.example.com"],"forward_scheme":"http","forward_host":"gone","forward_port":80,"enabled":true}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(npm.Close)
	var q string
	dock := fakeDocker(t, &q)
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Infra", Services: []config.Service{{
		Name: "NPM", Widget: config.Widget{"type": "npm", "url": npm.URL, "email": "a@b.c", "password": "pw"},
	}}}}
	if err := store.WriteInitial(cfg); err != nil {
		t.Fatal(err)
	}
	return New(store, monitor.New(store, dock, "/proc", "/sys"), dock, dir, nil).Handler()
}

func TestDiscoverTakesLinksFromNPM(t *testing.T) {
	h := withNPM(t)
	var resp struct {
		Containers []discover.Suggestion `json:"containers"`
	}
	json.Unmarshal(do(h, "GET", "/api/discover", "").Body.Bytes(), &resp)
	if len(resp.Containers) != 1 {
		t.Fatalf("suggestions: %+v", resp.Containers)
	}
	s := resp.Containers[0]
	if s.URL != "https://web.example.com" || s.URLGuessed || !s.URLFromNPM {
		t.Fatalf("suggestion: %+v", s)
	}
}

func TestTopologyEndpoint(t *testing.T) {
	h := withNPM(t)
	rec := do(h, "GET", "/api/topology", "")
	var g topology.Graph
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	if g.Sources.Docker == nil || *g.Sources.Docker != "" || g.Sources.NPM == nil || g.Sources.Kopia != nil {
		t.Fatalf("sources: %+v", g.Sources)
	}
	if len(g.Domains) != 2 || g.Domains[1].Target != "c:web" || g.Domains[0].Target != "" {
		t.Fatalf("domains: %+v", g.Domains)
	}
	if len(g.Issues) != 1 {
		t.Fatalf("issues: %+v", g.Issues)
	}
}

// Gatehouse takes NPM's place: its discovery API feeds discovery links and
// the topology, and it wins when both are set up.
func TestGatehouseFeedsDiscoveryAndTopology(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/foyer/widget":
			fmt.Fprint(w, `{"version":1,"stats":[],"items":[]}`)
		case "/api/discovery":
			fmt.Fprint(w, `{"hosts":[{"domains":["web.example.com"],"scheme":"http","forward_host":"web","forward_port":80,"enabled":true,"https":true,"state":"awake"}],
				"redirects":[],"certificates":[],"warn_days":14}`)
		}
	}))
	t.Cleanup(gh.Close)
	var q string
	dock := fakeDocker(t, &q)
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Infra", Services: []config.Service{
		{Name: "NPM", Widget: config.Widget{"type": "npm", "url": "http://127.0.0.1:1", "email": "a@b.c", "password": "pw"}},
		{Name: "Gatehouse", Widget: config.Widget{"type": "gatehouse", "url": gh.URL, "key": "k"}},
	}}}
	if err := store.WriteInitial(cfg); err != nil {
		t.Fatal(err)
	}
	h := New(store, monitor.New(store, dock, "/proc", "/sys"), dock, dir, nil).Handler()

	var resp struct {
		Containers []discover.Suggestion `json:"containers"`
	}
	json.Unmarshal(do(h, "GET", "/api/discover", "").Body.Bytes(), &resp)
	if len(resp.Containers) != 1 || resp.Containers[0].URL != "https://web.example.com" || !resp.Containers[0].URLFromNPM {
		t.Fatalf("suggestions: %+v", resp.Containers)
	}
	var g topology.Graph
	json.Unmarshal(do(h, "GET", "/api/topology", "").Body.Bytes(), &g)
	if g.Sources.NPM == nil || *g.Sources.NPM != "" || len(g.Domains) != 1 || g.Domains[0].Target != "c:web" {
		t.Fatalf("topology: %+v", g)
	}
}
