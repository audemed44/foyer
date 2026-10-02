package widgets

import (
	"context"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

// GatehouseData is Gatehouse's own card (the Foyer widget format, with Wake
// buttons for sleeping apps) plus its proxy hosts and certificates, which
// feed the topology, link discovery and certificate alerts as NPM's did.
type GatehouseData struct {
	AppWidget
	Proxy NPMData `json:"proxy"`
}

// gatehouse reads Gatehouse's card and its read-only discovery API.
// Settings: url (the admin port, e.g. http://gatehouse:8081), key (its
// token), warn_days (default: Gatehouse's own).
func gatehouse(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "key"); err != nil {
		return nil, err
	}
	base := w.String("url")
	auth := []string{"Authorization", "Bearer " + w.String("key")}

	var out GatehouseData
	if err := getJSON(ctx, join(base, WellKnownPath), &out.AppWidget, auth...); err != nil {
		return nil, err
	}
	if err := out.AppWidget.sanitize(); err != nil {
		return nil, err
	}

	var d struct {
		Hosts []struct {
			Domains     []string `json:"domains"`
			Scheme      string   `json:"scheme"`
			ForwardHost string   `json:"forward_host"`
			ForwardPort int      `json:"forward_port"`
			Enabled     bool     `json:"enabled"`
			HTTPS       bool     `json:"https"`
			Certificate string   `json:"certificate"`
			Container   string   `json:"container"`
			State       string   `json:"state"`
		} `json:"hosts"`
		Redirects    []struct{} `json:"redirects"`
		Certificates []struct {
			Name    string    `json:"name"`
			Domains []string  `json:"domains"`
			Source  string    `json:"source"`
			Expires time.Time `json:"expires"`
			Hosts   int       `json:"hosts"`
		} `json:"certificates"`
		WarnDays int `json:"warn_days"`
	}
	if err := getJSON(ctx, join(base, "/api/discovery"), &d, auth...); err != nil {
		return nil, err
	}
	p := NPMData{
		Hosts: []ProxyHost{}, Certificates: []Certificate{}, Redirects: len(d.Redirects),
		WarnDays: max(1, w.Int("warn_days", max(d.WarnDays, 14))),
	}
	for _, h := range d.Hosts {
		ph := ProxyHost{
			Domains: h.Domains, Scheme: h.Scheme, ForwardHost: h.ForwardHost, ForwardPort: h.ForwardPort,
			Enabled: h.Enabled, SSL: h.HTTPS, Certificate: h.Certificate, State: h.State, Container: h.Container,
		}
		if !ph.Enabled {
			p.Disabled++
		}
		p.Hosts = append(p.Hosts, ph)
	}
	now := time.Now()
	for _, c := range d.Certificates {
		provider := "other"
		if strings.HasPrefix(c.Source, "acme") || c.Source == "npm" {
			provider = "letsencrypt"
		}
		cert := Certificate{
			Name: c.Name, Domains: c.Domains, Provider: provider, Expires: c.Expires,
			Days: int(c.Expires.Sub(now).Hours() / 24), Hosts: c.Hosts,
		}
		if cert.Days < p.WarnDays {
			p.Expiring++
		}
		p.Certificates = append(p.Certificates, cert)
	}
	out.Proxy = p
	return out, nil
}

// IsApp reports whether a widget shows an app's own card, with its
// actions and uploads.
func IsApp(w config.Widget) bool {
	t := w.Type()
	return t == "app" || t == "gatehouse" || t == "lookout"
}

// AsApp returns the app card in a widget's data.
func AsApp(data any) (AppWidget, bool) {
	switch d := data.(type) {
	case AppWidget:
		return d, true
	case GatehouseData:
		return d.AppWidget, true
	case LookoutData:
		return d.AppWidget, true
	}
	return AppWidget{}, false
}

// AsProxy returns the reverse proxy's hosts and certificates in a widget's
// data, from Gatehouse or Nginx Proxy Manager.
func AsProxy(data any) (NPMData, bool) {
	switch d := data.(type) {
	case NPMData:
		return d, true
	case GatehouseData:
		return d.Proxy, true
	}
	return NPMData{}, false
}
