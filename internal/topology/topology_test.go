package topology

import (
	"strings"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/widgets"
)

var containers = []docker.Container{
	{ID: "1", Name: "shelfloom", State: "running", Labels: map[string]string{}},
	{ID: "2", Name: "kopia", State: "running", Published: []docker.PortMap{{Host: 51515, Container: 51515}}, Labels: map[string]string{}},
	{ID: "3", Name: "main-server-web-1", State: "running", Labels: map[string]string{"com.docker.compose.service": "web"}},
	{ID: "4", Name: "syncthing", State: "exited", Labels: map[string]string{}},
	{ID: "5", Name: "db", State: "running", Labels: map[string]string{}},
}

var details = map[string]docker.Details{
	"1": {Mounts: []docker.Mount{
		{Type: "bind", Source: "/home/u/stack/shelfloom/data", Destination: "/data", RW: true},
		{Type: "bind", Source: "/etc/localtime", Destination: "/etc/localtime"},
	}, Aliases: []string{"books"}},
	"2": {Mounts: []docker.Mount{
		{Type: "bind", Source: "/home/u/stack", Destination: "/data"},
		{Type: "bind", Source: "/home/u/kopia/config", Destination: "/app/config", RW: true},
	}},
	"4": {Mounts: []docker.Mount{{Type: "bind", Source: "/home/u/sync", Destination: "/var/syncthing", RW: true}}},
	"5": {Mounts: []docker.Mount{
		{Type: "volume", Name: "pgdata", Source: "/var/lib/docker/volumes/pgdata/_data", Destination: "/var/lib/postgresql", RW: true},
		{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
	}},
}

func TestResolve(t *testing.T) {
	cases := []struct {
		host   string
		port   int
		want   string
		onHost bool
	}{
		{"shelfloom", 8000, "shelfloom", false},
		{"SHELFLOOM", 8000, "shelfloom", false},
		{"web", 80, "main-server-web-1", false}, // compose service name
		{"books", 8000, "shelfloom", false},     // network alias
		{"host.docker.internal", 51515, "kopia", false},
		{"192.168.1.10", 51515, "kopia", false}, // the host's own address
		{"172.17.0.1", 9090, "", true},          // a process on the host
		{"paperless", 8000, "", false},          // nothing answers
	}
	for _, c := range cases {
		name, onHost := Resolve(c.host, c.port, containers, details)
		if name != c.want || onHost != c.onHost {
			t.Errorf("Resolve(%s:%d) = %q, %v; want %q, %v", c.host, c.port, name, onHost, c.want, c.onHost)
		}
	}
}

func TestHostPath(t *testing.T) {
	mounts := []docker.Mount{
		{Source: "/home/u/stack", Destination: "/data"},
		{Source: "/mnt/photos", Destination: "/data/photos"},
	}
	for in, want := range map[string]string{
		"/data":              "/home/u/stack",
		"/data/shelfloom":    "/home/u/stack/shelfloom",
		"/data/photos/2026":  "/mnt/photos/2026", // the deeper mount wins
		"/database/whatever": "",                 // not /data
	} {
		got, ok := hostPath(in, mounts)
		if got != want || ok != (want != "") {
			t.Errorf("hostPath(%s) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func build() Graph {
	last := time.Now().Add(-2 * time.Hour)
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Apps", Services: []config.Service{
		{Name: "Shelfloom", URL: "https://books.example.com", Icon: "shelfloom.png", Container: "shelfloom"},
	}}}
	return Build(Input{
		Config: cfg, Containers: containers, Details: details,
		NPM: &widgets.NPMData{WarnDays: 14, Hosts: []widgets.ProxyHost{
			{Domains: []string{"books.example.com"}, Scheme: "http", ForwardHost: "shelfloom", ForwardPort: 8000, Enabled: true, SSL: true, Certificate: "Wildcard"},
			{Domains: []string{"sync.example.com"}, Scheme: "http", ForwardHost: "syncthing", ForwardPort: 8384, Enabled: true},
			{Domains: []string{"old.example.com"}, Scheme: "http", ForwardHost: "paperless", ForwardPort: 8000, Enabled: true},
			{Domains: []string{"cockpit.example.com"}, Scheme: "https", ForwardHost: "172.17.0.1", ForwardPort: 9090, Enabled: true},
		}, Certificates: []widgets.Certificate{{Name: "Wildcard", Days: 5, Expires: time.Now().Add(5 * 24 * time.Hour)}}},
		Kopia:              &widgets.KopiaData{Sources: []widgets.KopiaSource{{Path: "/data", State: "ok", Last: &last}}},
		KopiaContainer:     "kopia",
		Syncthing:          &widgets.SyncthingData{Folders: []widgets.SyncFolder{{Label: "Phone", Path: "/var/syncthing/phone", State: "idle"}}},
		SyncthingContainer: "syncthing",
	})
}

func TestBuildLinksDomainsContainersAndStorage(t *testing.T) {
	g := build()
	targets := map[string]string{}
	for _, d := range g.Domains {
		targets[d.Name] = d.Target
	}
	if targets["books.example.com"] != "c:shelfloom" || targets["cockpit.example.com"] != "host" || targets["old.example.com"] != "" {
		t.Fatalf("targets: %v", targets)
	}
	if g.Host == nil || len(g.Host.Ports) != 1 || g.Host.Ports[0] != 9090 {
		t.Fatalf("host: %+v", g.Host)
	}
	for _, d := range g.Domains {
		if d.Name == "books.example.com" && (d.Service == nil || d.Service.Name != "Shelfloom" || d.CertDays == nil || *d.CertDays != 5) {
			t.Fatalf("books domain: %+v", d)
		}
	}

	byID := map[string]Storage{}
	for _, s := range g.Storage {
		byID[s.ID] = s
	}
	data := byID["p:/home/u/stack/shelfloom/data"]
	if data.Backup == nil || data.Backup.Partial || data.Backup.State != "ok" || !data.Written || data.Class != "data" {
		t.Fatalf("shelfloom data should be backed up via kopia's /data: %+v", data)
	}
	if byID["p:/etc/localtime"].Class != "system" || byID["p:/var/run/docker.sock"].Class != "socket" {
		t.Fatalf("classes: %+v", g.Storage)
	}
	if s := byID["p:/home/u/sync"]; s.Sync == nil || !s.Sync.Partial || s.Sync.Folder != "Phone" {
		t.Fatalf("sync: %+v", s)
	}
	if v := byID["v:pgdata"]; v.Backup != nil || !v.Written {
		t.Fatalf("volume: %+v", v)
	}
}

func TestBuildIssues(t *testing.T) {
	g := build()
	var texts []string
	for _, i := range g.Issues {
		texts = append(texts, i.Tone+": "+i.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{
		"bad: sync.example.com forwards to syncthing, which is exited",
		"bad: old.example.com forwards to paperless:8000, but no container answers",
		"warn: The certificate for books.example.com expires in 5 days",
		// pgdata; kopia's own config doesn't count, nor syncthing (not running).
		"warn: 1 data folder written by running containers",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing issue %q in:\n%s", want, all)
		}
	}
	if !strings.HasPrefix(texts[0], "bad") {
		t.Errorf("bad issues should come first: %v", texts)
	}
}
