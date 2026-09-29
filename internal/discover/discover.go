// Package discover suggests dashboard entries for running containers that
// aren't on the dashboard yet.
package discover

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
)

type Suggestion struct {
	Container   string `json:"container"`
	Image       string `json:"image"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	URL         string `json:"url,omitempty"`
	// URLGuessed is set when URL follows the pattern of existing services
	// (https://<name>.example.com) rather than coming from a label.
	URLGuessed bool `json:"url_guessed,omitempty"`
	// URLFromNPM is set when the link comes from an Nginx Proxy Manager host.
	URLFromNPM bool   `json:"url_from_npm,omitempty"`
	Ping       string `json:"ping,omitempty"`
	Group      string `json:"group,omitempty"`
	// Labelled means the container carries foyer.* or homepage.* labels.
	Labelled bool `json:"labelled,omitempty"`
	// Widget is set when the app serves a Foyer widget.
	Widget config.Widget `json:"widget,omitempty"`
}

// label reads foyer.<key>, falling back to Homepage's homepage.<alt> labels.
func label(c docker.Container, key string, alts ...string) string {
	if v := c.Labels["foyer."+key]; v != "" {
		return v
	}
	for _, alt := range append([]string{key}, alts...) {
		if v := c.Labels["homepage."+alt]; v != "" {
			return v
		}
	}
	return ""
}

// Linked returns the container names the dashboard already points at, via a
// service's container field or the host of its status check or link.
func Linked(cfg config.Config) map[string]bool {
	linked := map[string]bool{}
	for _, s := range cfg.Services() {
		if s.Container != "" {
			linked[s.Container] = true
		}
		for _, raw := range []string{s.Ping, s.URL} {
			if u, err := url.Parse(config.ExpandEnv(raw)); err == nil && u.Hostname() != "" {
				linked[u.Hostname()] = true
			}
		}
	}
	return linked
}

func Suggest(cfg config.Config, containers []docker.Container) []Suggestion {
	linked := Linked(cfg)
	suffix := domainSuffix(cfg)
	var out []Suggestion
	for _, c := range containers {
		if c.State != "running" || linked[c.Name] || slices.Contains(cfg.IgnoredContainers, c.Name) {
			continue
		}
		if strings.EqualFold(label(c, "hide"), "true") || isFoyer(c) {
			continue
		}
		base := c.Labels["com.docker.compose.service"]
		if base == "" {
			base = c.Name
		}
		s := Suggestion{
			Container:   c.Name,
			Image:       c.Image,
			Name:        label(c, "name"),
			Description: label(c, "description"),
			Icon:        label(c, "icon"),
			URL:         label(c, "url", "href"),
			Ping:        label(c, "ping", "siteMonitor"),
			Group:       label(c, "group"),
		}
		s.Labelled = s.Name != "" || s.URL != "" || s.Group != ""
		if s.Name == "" {
			s.Name = prettify(base)
		}
		if s.Icon == "" {
			name := imageName(c.Image)
			if name == "" {
				name = strings.ToLower(base) // image referenced only by digest
			}
			s.Icon = name + ".png"
		}
		if s.Ping == "" {
			if port := webPort(c.Ports); port > 0 {
				s.Ping = fmt.Sprintf("http://%s:%d", c.Name, port)
			}
		}
		// Only guess a link for something that serves HTTP; databases don't.
		if s.URL == "" && suffix != "" && s.Ping != "" {
			s.URL, s.URLGuessed = "https://"+strings.ToLower(base)+"."+suffix, true
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Labelled != out[j].Labelled {
			return out[i].Labelled
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if out == nil {
		out = []Suggestion{}
	}
	return out
}

func isFoyer(c docker.Container) bool {
	return imageName(c.Image) == "foyer"
}

// imageName is an image's repository name without registry, owner or tag:
// ghcr.io/linuxserver/qbittorrent:latest → qbittorrent.
func imageName(image string) string {
	if strings.HasPrefix(image, "sha256:") {
		return ""
	}
	name := image
	if i := strings.LastIndex(name, "@"); i >= 0 {
		name = name[:i]
	}
	name = name[strings.LastIndex(name, "/")+1:]
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

// prettify turns a container name into a display name: uptime-kuma → Uptime Kuma.
func prettify(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	if len(words) == 0 {
		return name
	}
	return strings.Join(words, " ")
}

// webPort picks the port most likely to serve the web UI.
func webPort(ports []int) int {
	for _, want := range []int{80, 8080, 3000, 8000, 5000, 9000, 8096, 8081, 443} {
		if slices.Contains(ports, want) {
			return want
		}
	}
	for _, p := range ports {
		// Skip well-known non-HTTP ports (databases, mail, DNS, …).
		if !slices.Contains([]int{21, 22, 25, 53, 110, 143, 993, 995, 1883, 3306, 5432, 6379, 27017}, p) {
			return p
		}
	}
	return 0
}

// domainSuffix finds the domain most existing services live under, e.g.
// "example.com" when links look like https://sonarr.example.com. It needs at
// least two services and a majority to count as a pattern.
func domainSuffix(cfg config.Config) string {
	counts := map[string]int{}
	total := 0
	for _, s := range cfg.Services() {
		u, err := url.Parse(s.URL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		total++
		host := u.Hostname()
		if i := strings.Index(host, "."); i > 0 && strings.Count(host, ".") >= 2 {
			counts[host[i+1:]]++
		}
	}
	best, n := "", 0
	for suffix, c := range counts {
		if c > n || (c == n && suffix < best) {
			best, n = suffix, c
		}
	}
	if n >= 2 && n*2 > total {
		return best
	}
	return ""
}
