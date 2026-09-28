package discover

import (
	"testing"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
)

func cfg() config.Config {
	c := config.Default()
	c.Groups = []config.Group{{Name: "Apps", Services: []config.Service{
		{Name: "Sonarr", URL: "https://sonarr.home.example.com", Ping: "http://sonarr:8989"},
		{Name: "Radarr", URL: "https://radarr.home.example.com", Container: "radarr"},
		{Name: "Plex", URL: "http://plex:32400/web"},
	}}}
	c.IgnoredContainers = []string{"watchtower"}
	_ = c.Normalize()
	return c
}

func running(name, image string, ports []int, labels map[string]string) docker.Container {
	return docker.Container{Name: name, Image: image, State: "running", Ports: ports, Labels: labels}
}

func TestSuggest(t *testing.T) {
	got := Suggest(cfg(), []docker.Container{
		running("sonarr", "lscr.io/linuxserver/sonarr", []int{8989}, nil),    // linked by ping
		running("radarr", "lscr.io/linuxserver/radarr", []int{7878}, nil),    // linked by container
		running("plex", "plexinc/pms-docker", []int{32400}, nil),             // linked by url
		running("watchtower", "containrrr/watchtower", nil, nil),             // ignored
		running("foyer", "ghcr.io/audemed44/foyer:latest", []int{8080}, nil), // itself
		{Name: "stopped", State: "exited"},
		running("uptime-kuma", "louislam/uptime-kuma:1", []int{3001}, nil),
		running("romm-db", "mariadb:11", []int{3306}, nil),
		running("jelly", "jellyfin/jellyfin", []int{1900, 7359, 8096}, map[string]string{
			"homepage.name": "Jellyfin", "homepage.group": "Media", "homepage.href": "https://tv.example.com",
			"homepage.icon": "jellyfin.svg",
		}),
		running("x", "img", nil, map[string]string{"foyer.hide": "true"}),
	})
	if len(got) != 3 {
		t.Fatalf("got %d suggestions: %+v", len(got), got)
	}

	jelly := got[0]
	if !jelly.Labelled || jelly.Name != "Jellyfin" || jelly.Group != "Media" || jelly.URL != "https://tv.example.com" ||
		jelly.URLGuessed || jelly.Icon != "jellyfin.svg" || jelly.Ping != "http://jelly:8096" {
		t.Fatalf("labelled suggestion should come first with its labels: %+v", jelly)
	}

	db := got[1]
	if db.Name != "Romm Db" || db.Icon != "mariadb.png" || db.Ping != "" || db.URL != "" {
		t.Fatalf("romm-db: %+v", db)
	}

	kuma := got[2]
	if kuma.Name != "Uptime Kuma" || kuma.Icon != "uptime-kuma.png" || kuma.Ping != "http://uptime-kuma:3001" {
		t.Fatalf("uptime-kuma: %+v", kuma)
	}
	if kuma.URL != "https://uptime-kuma.home.example.com" || !kuma.URLGuessed {
		t.Fatalf("url should follow the existing pattern: %+v", kuma)
	}
}

func TestDomainSuffixNeedsAPattern(t *testing.T) {
	c := config.Default()
	c.Groups = []config.Group{{Name: "g", Services: []config.Service{
		{Name: "a", URL: "https://a.one.com"},
		{Name: "b", URL: "https://b.two.com"},
	}}}
	_ = c.Normalize()
	if s := domainSuffix(c); s != "" {
		t.Fatalf("no majority, got %q", s)
	}
}

func TestImageName(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/linuxserver/qbittorrent:latest": "qbittorrent",
		"nginx":                                  "nginx",
		"localhost:5000/team/App:v1":             "app",
		"redis@sha256:abc":                       "redis",
		"sha256:0123abcd":                        "",
	} {
		if got := imageName(in); got != want {
			t.Errorf("imageName(%q) = %q, want %q", in, got, want)
		}
	}
}
