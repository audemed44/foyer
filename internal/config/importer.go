package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FindHomepage accepts either gethomepage.dev's config folder or the folder
// that contains it, and returns the config folder.
func FindHomepage(root string) (string, bool) {
	for _, dir := range []string{root, filepath.Join(root, "config")} {
		if _, err := os.Stat(filepath.Join(dir, "services.yaml")); err == nil {
			return dir, true
		}
	}
	return "", false
}

// entry is one `name: value` item from homepage's list-of-maps YAML.
type entry struct {
	name  string
	value yaml.Node
}

func readEntries(path string) ([]entry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	return nodeEntries(root.Content[0]), nil
}

// nodeEntries flattens `[{a: x}, {b: y}]` into ordered (name, value) pairs.
func nodeEntries(list *yaml.Node) []entry {
	var out []entry
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range list.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(item.Content); i += 2 {
			out = append(out, entry{item.Content[i].Value, *item.Content[i+1]})
		}
	}
	return out
}

type hpService struct {
	Href        string         `yaml:"href"`
	Description string         `yaml:"description"`
	Icon        string         `yaml:"icon"`
	Ping        string         `yaml:"ping"`
	SiteMonitor string         `yaml:"siteMonitor"`
	Container   string         `yaml:"container"`
	Widget      map[string]any `yaml:"widget"`
}

// ImportHomepage converts a gethomepage.dev config folder into a Foyer config.
func ImportHomepage(dir string) (Config, error) {
	cfg := Default()

	var settings struct {
		Title      string `yaml:"title"`
		Theme      string `yaml:"theme"`
		Background any    `yaml:"background"`
	}
	if data, err := os.ReadFile(filepath.Join(dir, "settings.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &settings); err != nil {
			return cfg, fmt.Errorf("settings.yaml: %w", err)
		}
	}
	if settings.Title != "" {
		cfg.Title = settings.Title
	}
	if settings.Theme == "light" {
		cfg.Theme.Mode = "light"
	}
	background := ""
	switch bg := settings.Background.(type) {
	case string:
		background = bg
	case map[string]any:
		background, _ = bg["image"].(string)
	}
	if background != "" && !strings.Contains(background, "://") {
		background = "/" + strings.TrimPrefix(background, "/")
	}
	cfg.Theme.Background = background

	groups, err := readEntries(filepath.Join(dir, "services.yaml"))
	if err != nil {
		return cfg, err
	}
	for _, g := range groups {
		// Homepage's per-group columns aren't carried over: Foyer sizes each
		// group to its contents and packs small groups side by side.
		group := Group{Name: g.name, Services: []Service{}}
		for _, s := range nodeEntries(&g.value) {
			var spec hpService
			if err := s.value.Decode(&spec); err != nil {
				slog.Warn("skipping homepage service", "name", s.name, "err", err)
				continue
			}
			svc := Service{
				Name:        s.name,
				URL:         strings.TrimSpace(spec.Href),
				Description: strings.TrimSpace(spec.Description),
				Icon:        strings.TrimSpace(spec.Icon),
				Ping:        strings.TrimSpace(spec.Ping),
				Container:   strings.TrimSpace(spec.Container),
			}
			if svc.Ping == "" {
				svc.Ping = strings.TrimSpace(spec.SiteMonitor)
			}
			svc.Widget = convertWidget(spec.Widget)
			group.Services = append(group.Services, svc)
		}
		cfg.Groups = append(cfg.Groups, group)
	}

	widgets, err := readEntries(filepath.Join(dir, "widgets.yaml"))
	if err != nil {
		return cfg, err
	}
	for _, w := range widgets {
		var spec map[string]any
		_ = w.value.Decode(&spec)
		switch w.name {
		case "search":
			provider, _ := spec["provider"].(string)
			switch provider {
			case "google", "duckduckgo", "bing", "kagi":
				cfg.Header.Search.Provider = provider
			case "custom":
				if u, ok := spec["url"].(string); ok {
					cfg.Header.Search.Provider, cfg.Header.Search.URL = "custom", u
				}
			}
		case "resources":
			sys := &cfg.Header.System
			sys.CPU = spec["cpu"] == true
			sys.Memory = spec["memory"] == true
			sys.Temperature = spec["cputemp"] == true
			sys.Uptime = spec["uptime"] == true
		}
	}

	bookmarks, err := readEntries(filepath.Join(dir, "bookmarks.yaml"))
	if err != nil {
		return cfg, err
	}
	for _, b := range bookmarks {
		group := BookmarkGroup{Name: b.name, Links: []Bookmark{}}
		for _, link := range nodeEntries(&b.value) {
			var specs []struct {
				Href string `yaml:"href"`
				Abbr string `yaml:"abbr"`
				Icon string `yaml:"icon"`
			}
			if err := link.value.Decode(&specs); err != nil || len(specs) == 0 || specs[0].Href == "" {
				continue
			}
			group.Links = append(group.Links, Bookmark{
				Name: link.name, URL: specs[0].Href, Abbr: specs[0].Abbr, Icon: specs[0].Icon,
			})
		}
		cfg.Bookmarks = append(cfg.Bookmarks, group)
	}
	return cfg, cfg.Normalize()
}

func convertWidget(w map[string]any) Widget {
	if w == nil {
		return nil
	}
	switch w["type"] {
	case "uptimekuma":
		return Widget{"type": "uptimekuma", "url": w["url"], "slug": w["slug"]}
	case "speedtest":
		out := Widget{"type": "speedtest", "url": w["url"]}
		if v, ok := w["version"]; ok {
			out["version"] = v
		}
		if k, ok := w["key"]; ok {
			out["key"] = k
		}
		return out
	case "calendar":
		integrations, _ := w["integrations"].([]any)
		for _, raw := range integrations {
			i, _ := raw.(map[string]any)
			if i["type"] == "ical" {
				out := Widget{"type": "calendar", "url": i["url"]}
				if n, ok := w["maxEvents"]; ok {
					out["max_events"] = n
				}
				return out
			}
		}
	}
	slog.Info("skipping unsupported homepage widget", "type", w["type"])
	return nil
}

// CopyAssets copies homepage's icons/ and images/ folders (siblings of its
// config folder) next to the Foyer config, without overwriting files.
func CopyAssets(homepageConfigDir, foyerDir string) {
	root := filepath.Dir(homepageConfigDir)
	for _, name := range []string{"icons", "images"} {
		src := filepath.Join(root, name)
		entries, err := os.ReadDir(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(foyerDir, name)
		_ = os.MkdirAll(dst, 0o755)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				slog.Warn("could not copy asset", "file", e.Name(), "err", err)
			}
		}
	}
}

func copyFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
