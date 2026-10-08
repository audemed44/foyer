// Package config holds the dashboard schema and the YAML file that stores it.
package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Widget is a service widget. "type" picks the kind; other keys depend on it.
type Widget map[string]any

func (w Widget) Type() string { return w.String("type") }

func (w Widget) String(key string) string {
	if v, ok := w[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func (w Widget) Int(key string, fallback int) int {
	switch v := w[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return fallback
}

type Service struct {
	ID          string `yaml:"id,omitempty" json:"id"`
	Name        string `yaml:"name" json:"name"`
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Icon        string `yaml:"icon,omitempty" json:"icon,omitempty"`
	// Ping is checked server-side for the status dot; any response below 500 is up.
	Ping string `yaml:"ping,omitempty" json:"ping,omitempty"`
	// Container is a Docker container name, for running/health state.
	Container string `yaml:"container,omitempty" json:"container,omitempty"`
	Widget    Widget `yaml:"widget,omitempty" json:"widget,omitempty"`
}

type Group struct {
	ID        string    `yaml:"id,omitempty" json:"id"`
	Name      string    `yaml:"name" json:"name"`
	Columns   int       `yaml:"columns,omitempty" json:"columns,omitempty"`
	Collapsed bool      `yaml:"collapsed,omitempty" json:"collapsed"`
	Services  []Service `yaml:"services" json:"services"`
}

type Bookmark struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	Abbr string `yaml:"abbr,omitempty" json:"abbr,omitempty"`
	Icon string `yaml:"icon,omitempty" json:"icon,omitempty"`
}

type BookmarkGroup struct {
	Name  string     `yaml:"name" json:"name"`
	Links []Bookmark `yaml:"links" json:"links"`
}

type Theme struct {
	Mode           string  `yaml:"mode" json:"mode"`       // dark | light | auto
	Accent         string  `yaml:"accent" json:"accent"`   // #rrggbb
	Font           string  `yaml:"font" json:"font"`       // mono | sans | serif
	Cards          string  `yaml:"cards" json:"cards"`     // outline | filled | glass
	Density        string  `yaml:"density" json:"density"` // comfortable | compact
	Columns        int     `yaml:"columns" json:"columns"`
	Background     string  `yaml:"background" json:"background"`
	BackgroundDim  float64 `yaml:"background_dim" json:"background_dim"`
	BackgroundBlur int     `yaml:"background_blur" json:"background_blur"`
	CustomCSS      string  `yaml:"custom_css" json:"custom_css"`
}

type Search struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Provider string `yaml:"provider" json:"provider"` // google | duckduckgo | bing | kagi | custom
	URL      string `yaml:"url" json:"url"`           // for custom: the query is appended
}

type System struct {
	Enabled     bool `yaml:"enabled" json:"enabled"`
	CPU         bool `yaml:"cpu" json:"cpu"`
	Memory      bool `yaml:"memory" json:"memory"`
	Temperature bool `yaml:"temperature" json:"temperature"`
	Uptime      bool `yaml:"uptime" json:"uptime"`
	// Disks are paths inside the container; mount host folders read-only to watch them.
	Disks []string `yaml:"disks" json:"disks"`
}

type Header struct {
	Greeting bool   `yaml:"greeting" json:"greeting"`
	Name     string `yaml:"name" json:"name"`
	Clock    bool   `yaml:"clock" json:"clock"`
	Clock24h bool   `yaml:"clock_24h" json:"clock_24h"`
	Search   Search `yaml:"search" json:"search"`
	System   System `yaml:"system" json:"system"`
}

// Alerts sends notifications through Apprise. Empty AppriseURL turns them off.
type Alerts struct {
	// AppriseURL is an Apprise API notify endpoint, e.g.
	// http://apprise-api:8000/notify/foyer (a key saved in Apprise).
	AppriseURL string `yaml:"apprise_url" json:"apprise_url"`
	Tag        string `yaml:"tag" json:"tag"`
	// DownAfter is how many failed checks in a row count as down.
	DownAfter    int  `yaml:"down_after" json:"down_after"`
	Services     bool `yaml:"services" json:"services"`         // dashboard services down / unhealthy
	Containers   bool `yaml:"containers" json:"containers"`     // any container crashing or unhealthy
	Backups      bool `yaml:"backups" json:"backups"`           // backup sources (Keep or Kopia) stale or failing
	Sync         bool `yaml:"sync" json:"sync"`                 // Syncthing folder errors
	Certificates bool `yaml:"certificates" json:"certificates"` // NPM certificates near expiry
}

type Config struct {
	Title        string          `yaml:"title" json:"title"`
	OpenInNewTab bool            `yaml:"open_in_new_tab" json:"open_in_new_tab"`
	PingInterval int             `yaml:"ping_interval" json:"ping_interval"`
	Theme        Theme           `yaml:"theme" json:"theme"`
	Header       Header          `yaml:"header" json:"header"`
	Groups       []Group         `yaml:"groups" json:"groups"`
	Bookmarks    []BookmarkGroup `yaml:"bookmarks" json:"bookmarks"`
	// IgnoredContainers are never suggested as new services in edit mode.
	IgnoredContainers []string `yaml:"ignored_containers" json:"ignored_containers"`
	Alerts            Alerts   `yaml:"alerts" json:"alerts"`
}

// Default is the starting point every config file is decoded over, so keys
// missing from the file keep these values.
func Default() Config {
	return Config{
		Title:        "Foyer",
		OpenInNewTab: true,
		PingInterval: 30,
		Theme: Theme{
			Mode: "dark", Accent: "#2563ff", Font: "sans", Cards: "outline",
			Density: "comfortable", Columns: 4, BackgroundDim: 0.75,
		},
		Header: Header{
			Greeting: true, Clock: true, Clock24h: true,
			Search: Search{Enabled: true, Provider: "google"},
			System: System{
				Enabled: true, CPU: true, Memory: true, Temperature: true, Uptime: true,
				Disks: []string{"/"},
			},
		},
		Groups:            []Group{},
		Bookmarks:         []BookmarkGroup{},
		IgnoredContainers: []string{},
		Alerts: Alerts{
			DownAfter: 2, Services: true, Backups: true, Sync: true, Certificates: true,
		},
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func oneOf(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, got %q", field, strings.Join(allowed, ", "), value)
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

// Normalize validates enum fields, clamps numbers and assigns ids.
func (c *Config) Normalize() error {
	t := &c.Theme
	checks := []error{
		oneOf("theme.mode", t.Mode, "dark", "light", "auto"),
		oneOf("theme.font", t.Font, "mono", "sans", "serif"),
		oneOf("theme.cards", t.Cards, "outline", "filled", "glass"),
		oneOf("theme.density", t.Density, "comfortable", "compact"),
		oneOf("header.search.provider", c.Header.Search.Provider,
			"google", "duckduckgo", "bing", "kagi", "custom"),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if !hexColor.MatchString(t.Accent) {
		return fmt.Errorf("theme.accent must be a #rrggbb colour, got %q", t.Accent)
	}
	t.Columns = clamp(t.Columns, 1, 6)
	t.BackgroundBlur = clamp(t.BackgroundBlur, 0, 40)
	t.BackgroundDim = max(0, min(1, t.BackgroundDim))
	c.PingInterval = clamp(c.PingInterval, 5, 3600)
	c.Alerts.DownAfter = clamp(c.Alerts.DownAfter, 1, 20)

	seen := map[string]bool{}
	unique := func(base string) string {
		id := base
		for n := 2; seen[id]; n++ {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		seen[id] = true
		return id
	}
	for gi := range c.Groups {
		g := &c.Groups[gi]
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("group %d has no name", gi+1)
		}
		g.Columns = clamp(g.Columns, 0, 6)
		g.ID = unique("group-" + slug(g.Name))
		if g.Services == nil {
			g.Services = []Service{}
		}
		for si := range g.Services {
			s := &g.Services[si]
			if strings.TrimSpace(s.Name) == "" {
				return fmt.Errorf("a service in group %q has no name", g.Name)
			}
			s.ID = unique(slug(s.Name))
			if s.Widget != nil && s.Widget.Type() == "" {
				return fmt.Errorf("service %q: widget needs a type", s.Name)
			}
		}
	}
	if c.IgnoredContainers == nil {
		c.IgnoredContainers = []string{}
	}
	for bi := range c.Bookmarks {
		if c.Bookmarks[bi].Links == nil {
			c.Bookmarks[bi].Links = []Bookmark{}
		}
	}
	return nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	out := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if out == "" {
		return "item"
	}
	return out
}

func (c Config) Services() []*Service {
	var out []*Service
	for gi := range c.Groups {
		for si := range c.Groups[gi].Services {
			out = append(out, &c.Groups[gi].Services[si])
		}
	}
	return out
}

func (c Config) Service(id string) *Service {
	for _, s := range c.Services() {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// SecretMask stands in for secret widget values sent to the editor.
const SecretMask = "__foyer_secret__"

var (
	secretKey = regexp.MustCompile(`(?i)(key|token|password|secret|pass)$`)
	envRef    = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
)

// Public is what anyone who can open the dashboard sees: widget settings
// (URLs, keys) never leave the server, only the widget's type and size.
func (c Config) Public() Config {
	out := c.clone()
	for _, s := range out.Services() {
		if s.Widget != nil {
			w := Widget{"type": s.Widget.Type()}
			if span, ok := s.Widget["span"]; ok {
				w["span"] = span
			}
			s.Widget = w
		}
		s.Ping = ""
	}
	// Where alerts go is a setting like any other widget URL.
	out.Alerts.AppriseURL, out.Alerts.Tag = "", ""
	return out
}

// Masked is the editor's view: everything except secret values. A `${ENV}`
// reference isn't a secret, so it's shown as is.
func (c Config) Masked() Config {
	out := c.clone()
	for _, s := range out.Services() {
		for k, v := range s.Widget {
			if str, ok := v.(string); ok && str != "" && secretKey.MatchString(k) && !envRef.MatchString(str) {
				s.Widget[k] = SecretMask
			}
		}
	}
	return out
}

// RestoreSecrets puts back values the editor received masked. Services are
// matched by id (sent back unchanged by the editor), then by name.
func (c *Config) RestoreSecrets(previous Config) {
	byID, byName := map[string]*Service{}, map[string]*Service{}
	for _, s := range previous.Services() {
		byID[s.ID] = s
		byName[s.Name] = s
	}
	for _, s := range c.Services() {
		old := byID[s.ID]
		if old == nil {
			old = byName[s.Name]
		}
		for k, v := range s.Widget {
			if v != SecretMask {
				continue
			}
			if old != nil && old.Widget != nil && old.Widget[k] != nil {
				s.Widget[k] = old.Widget[k]
			} else {
				delete(s.Widget, k)
			}
		}
	}
}

func (c Config) clone() Config {
	out := c
	out.Groups = make([]Group, len(c.Groups))
	for gi, g := range c.Groups {
		g.Services = append([]Service(nil), g.Services...)
		for si := range g.Services {
			if w := g.Services[si].Widget; w != nil {
				copied := make(Widget, len(w))
				for k, v := range w {
					copied[k] = v
				}
				g.Services[si].Widget = copied
			}
		}
		if g.Services == nil {
			g.Services = []Service{}
		}
		out.Groups[gi] = g
	}
	return out
}
