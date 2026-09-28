package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAssignsUniqueIDs(t *testing.T) {
	cfg := Default()
	cfg.Groups = []Group{
		{Name: "Media", Services: []Service{{Name: "Plex"}, {Name: "plex"}}},
		{Name: "Media"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := []string{cfg.Groups[0].Services[0].ID, cfg.Groups[0].Services[1].ID}; got[0] != "plex" || got[1] != "plex-2" {
		t.Fatalf("service ids = %v", got)
	}
	if cfg.Groups[0].ID == cfg.Groups[1].ID {
		t.Fatal("group ids collide")
	}
	if cfg.Groups[1].Services == nil {
		t.Fatal("nil services should become an empty list")
	}
}

func TestNormalizeRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"accent": func(c *Config) { c.Theme.Accent = "blue" },
		"mode":   func(c *Config) { c.Theme.Mode = "sepia" },
		"noname": func(c *Config) { c.Groups = []Group{{Name: " "}} },
		"widget": func(c *Config) {
			c.Groups = []Group{{Name: "g", Services: []Service{{Name: "s", Widget: Widget{"url": "x"}}}}}
		},
		"search": func(c *Config) { c.Header.Search.Provider = "yahoo" },
	}
	for name, mutate := range cases {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Normalize(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeClamps(t *testing.T) {
	cfg := Default()
	cfg.Theme.Columns = 40
	cfg.PingInterval = 1
	cfg.Theme.BackgroundDim = 3
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Columns != 6 || cfg.PingInterval != 5 || cfg.Theme.BackgroundDim != 1 {
		t.Fatalf("not clamped: %+v %d", cfg.Theme, cfg.PingInterval)
	}
}

func withWidget(w Widget) Config {
	cfg := Default()
	cfg.Groups = []Group{{Name: "g", Services: []Service{{Name: "Speed", Ping: "http://internal", Widget: w}}}}
	_ = cfg.Normalize()
	return cfg
}

func TestPublicHidesWidgetSettingsAndPings(t *testing.T) {
	cfg := withWidget(Widget{"type": "speedtest", "url": "http://internal", "key": "s3cret", "span": 3})
	pub := cfg.Public()
	svc := pub.Groups[0].Services[0]
	if svc.Ping != "" || len(svc.Widget) != 2 || svc.Widget["span"] != 3 {
		t.Fatalf("public service leaks settings: %+v", svc)
	}
	if cfg.Groups[0].Services[0].Widget["key"] != "s3cret" {
		t.Fatal("Public modified the original")
	}
}

func TestMaskAndRestoreSecrets(t *testing.T) {
	cfg := withWidget(Widget{"type": "speedtest", "key": "s3cret", "api_token": "${TOKEN}", "url": "http://x"})
	masked := cfg.Masked()
	w := masked.Groups[0].Services[0].Widget
	if w["key"] != SecretMask || w["api_token"] != "${TOKEN}" || w["url"] != "http://x" {
		t.Fatalf("masked = %v", w)
	}
	if cfg.Groups[0].Services[0].Widget["key"] != "s3cret" {
		t.Fatal("Masked modified the original")
	}

	// Renamed in the editor: matched by id.
	masked.Groups[0].Services[0].Name = "Speedtest"
	masked.RestoreSecrets(cfg)
	if got := masked.Groups[0].Services[0].Widget["key"]; got != "s3cret" {
		t.Fatalf("restored key = %v", got)
	}

	// A new service can't borrow a secret: the mask is dropped.
	fresh := withWidget(Widget{"type": "speedtest", "key": SecretMask})
	fresh.Groups[0].Services[0].ID = "other"
	fresh.Groups[0].Services[0].Name = "Other"
	fresh.RestoreSecrets(cfg)
	if _, ok := fresh.Groups[0].Services[0].Widget["key"]; ok {
		t.Fatal("mask should be removed when there is nothing to restore")
	}
}

func TestStoreSaveReloadAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foyer.yaml")
	store := NewStore(path)
	cfg := withWidget(Widget{"type": "speedtest", "key": "s3cret"})
	if err := store.WriteInitial(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get()
	if err != nil || loaded.Groups[0].Services[0].Widget["key"] != "s3cret" {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}

	edited := loaded.Masked()
	edited.Title = "Edited"
	if _, err := store.Save(edited); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "s3cret") || strings.Contains(string(data), SecretMask) {
		t.Fatalf("saved file lost the secret:\n%s", data)
	}
	if strings.Contains(string(data), "id:") {
		t.Fatal("ids should not be written")
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("expected a backup")
	}

	// A broken hand edit keeps the last good config and reports the error.
	os.WriteFile(path, []byte("theme: {accent: nope}\n"), 0o644)
	future := time.Now().Add(time.Minute)
	os.Chtimes(path, future, future)
	got, err := store.Get()
	if err == nil || got.Title != "Edited" {
		t.Fatalf("got %q, err %v", got.Title, err)
	}
}

func TestParseKeepsDefaultsForMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foyer.yaml")
	os.WriteFile(path, []byte("title: Mine\nheader:\n  clock: false\n"), 0o644)
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Mine" || cfg.Header.Clock || !cfg.Header.Greeting || cfg.Theme.Accent != "#2563ff" {
		t.Fatalf("defaults not kept: %+v", cfg.Header)
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOYER_TEST", "abc")
	if got := ExpandEnv("x-${FOYER_TEST}-${FOYER_MISSING}"); got != "x-abc-" {
		t.Fatalf("got %q", got)
	}
}

func TestImportHomepage(t *testing.T) {
	dir, ok := FindHomepage("testdata/homepage")
	if !ok {
		t.Fatal("homepage config not found")
	}
	cfg, err := ImportHomepage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Home Lab" || cfg.Theme.Mode != "light" || cfg.Theme.Background != "/images/bg.jpg" {
		t.Fatalf("settings: %q %q %q", cfg.Title, cfg.Theme.Mode, cfg.Theme.Background)
	}
	if len(cfg.Groups) != 2 || cfg.Groups[0].Columns != 0 {
		t.Fatalf("groups: %+v", cfg.Groups)
	}
	jelly := cfg.Groups[0].Services[0]
	if jelly.URL != "https://jelly.example.com" || jelly.Ping != "http://jellyfin:8096" || jelly.Icon != "jellyfin.png" {
		t.Fatalf("jellyfin: %+v", jelly)
	}
	speed := cfg.Groups[0].Services[1].Widget
	if speed.Type() != "speedtest" || speed["key"] != "abc123" || speed.Int("version", 0) != 2 {
		t.Fatalf("speedtest widget: %v", speed)
	}
	cal := cfg.Groups[1].Services[0].Widget
	if cal.String("url") != "http://tracker/cal.ics" || cal.Int("max_events", 0) != 5 {
		t.Fatalf("calendar widget: %v", cal)
	}
	if cfg.Groups[1].Services[1].Widget != nil {
		t.Fatal("unsupported widgets should be dropped")
	}
	if cfg.Header.Search.Provider != "duckduckgo" || cfg.Header.System.Uptime || !cfg.Header.System.CPU {
		t.Fatalf("header: %+v", cfg.Header)
	}
	if len(cfg.Bookmarks) != 1 || cfg.Bookmarks[0].Links[0].Abbr != "GH" {
		t.Fatalf("bookmarks: %+v", cfg.Bookmarks)
	}

	out := t.TempDir()
	CopyAssets(dir, out)
	if _, err := os.Stat(filepath.Join(out, "icons", "app.png")); err != nil {
		t.Fatal("icons not copied")
	}
}
