package widgets

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type ProxyHost struct {
	Domains     []string `json:"domains"`
	Scheme      string   `json:"scheme"`
	ForwardHost string   `json:"forward_host"`
	ForwardPort int      `json:"forward_port"`
	Enabled     bool     `json:"enabled"`
	SSL         bool     `json:"ssl"`
	Certificate string   `json:"certificate,omitempty"`
	// Error is nginx's complaint about this host's config, if any.
	Error string `json:"error,omitempty"`
	// State is Gatehouse's scale-to-zero state: awake, sleeping, waking or
	// stopping. Empty for NPM.
	State string `json:"state,omitempty"`
}

type Certificate struct {
	Name     string    `json:"name"`
	Domains  []string  `json:"domains"`
	Provider string    `json:"provider"` // letsencrypt | other
	Expires  time.Time `json:"expires"`
	Days     int       `json:"days"` // until expiry; negative once expired
	Hosts    int       `json:"hosts"`
}

type NPMData struct {
	Hosts        []ProxyHost   `json:"hosts"`
	Certificates []Certificate `json:"certificates"`
	Redirects    int           `json:"redirects"`
	Streams      int           `json:"streams"`
	Disabled     int           `json:"disabled"`
	Errors       int           `json:"errors"`
	// Expiring counts certificates within warn_days (default 14) of expiry.
	Expiring int `json:"expiring"`
	WarnDays int `json:"warn_days"`
}

type npmToken struct {
	token   string
	expires time.Time
}

// npmTokens caches logins per instance and user; NPM tokens last a day.
var npmTokens = struct {
	sync.Mutex
	m map[string]npmToken
}{m: map[string]npmToken{}}

func npmLogin(ctx context.Context, base, email, password string) (string, error) {
	key := base + "\x00" + email + "\x00" + password
	npmTokens.Lock()
	t, ok := npmTokens.m[key]
	npmTokens.Unlock()
	if ok && time.Until(t.expires) > 5*time.Minute {
		return t.token, nil
	}
	var resp struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
	}
	err := postJSON(ctx, join(base, "/api/tokens"), map[string]string{"identity": email, "secret": password}, &resp)
	if err != nil {
		return "", err
	}
	expires, err := time.Parse(time.RFC3339, resp.Expires)
	if err != nil {
		expires = time.Now().Add(time.Hour)
	}
	npmTokens.Lock()
	npmTokens.m[key] = npmToken{resp.Token, expires}
	npmTokens.Unlock()
	return resp.Token, nil
}

// npmTime reads NPM's "2006-01-02 15:04:05" timestamps, which are UTC.
func npmTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

// npm reads proxy hosts and certificates from Nginx Proxy Manager.
// Settings: url (the admin port, e.g. http://npm:81), email, password,
// warn_days (default 14).
func npm(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "email", "password"); err != nil {
		return nil, err
	}
	base := w.String("url")
	token, err := npmLogin(ctx, base, w.String("email"), w.String("password"))
	if err != nil {
		return nil, err
	}
	get := func(path string, out any) error {
		return getJSON(ctx, join(base, path), out, "Authorization", "Bearer "+token)
	}

	var hosts []struct {
		DomainNames   []string `json:"domain_names"`
		ForwardScheme string   `json:"forward_scheme"`
		ForwardHost   string   `json:"forward_host"`
		ForwardPort   int      `json:"forward_port"`
		Enabled       any      `json:"enabled"` // bool, or 0/1 on older versions
		CertificateID any      `json:"certificate_id"`
		Meta          struct {
			NginxOnline *bool   `json:"nginx_online"`
			NginxErr    *string `json:"nginx_err"`
		} `json:"meta"`
		Certificate *struct {
			NiceName string `json:"nice_name"`
		} `json:"certificate"`
	}
	if err := get("/api/nginx/proxy-hosts?expand=certificate", &hosts); err != nil {
		return nil, err
	}
	var certs []struct {
		ID          int      `json:"id"`
		NiceName    string   `json:"nice_name"`
		DomainNames []string `json:"domain_names"`
		Provider    string   `json:"provider"`
		ExpiresOn   string   `json:"expires_on"`
	}
	if err := get("/api/nginx/certificates", &certs); err != nil {
		return nil, err
	}
	var redirects, streams []struct{}
	_ = get("/api/nginx/redirection-hosts", &redirects)
	_ = get("/api/nginx/streams", &streams)

	warnDays := max(1, w.Int("warn_days", 14))
	out := NPMData{
		Hosts: []ProxyHost{}, Certificates: []Certificate{}, WarnDays: warnDays,
		Redirects: len(redirects), Streams: len(streams),
	}
	certUse := map[int]int{}
	for _, h := range hosts {
		ph := ProxyHost{
			Domains: h.DomainNames, Scheme: h.ForwardScheme, ForwardHost: h.ForwardHost,
			ForwardPort: h.ForwardPort, Enabled: truthy(h.Enabled),
		}
		if id := intOf(h.CertificateID); id > 0 {
			ph.SSL = true
			certUse[id]++
		}
		if h.Certificate != nil {
			ph.Certificate = h.Certificate.NiceName
		}
		if h.Meta.NginxOnline != nil && !*h.Meta.NginxOnline {
			ph.Error = "nginx rejected the config"
			if h.Meta.NginxErr != nil && *h.Meta.NginxErr != "" {
				ph.Error = clip(*h.Meta.NginxErr, 200)
			}
		}
		if !ph.Enabled {
			out.Disabled++
		}
		if ph.Error != "" {
			out.Errors++
		}
		out.Hosts = append(out.Hosts, ph)
	}
	now := time.Now()
	for _, c := range certs {
		expires := npmTime(c.ExpiresOn)
		cert := Certificate{
			Name: c.NiceName, Domains: c.DomainNames, Provider: c.Provider,
			Expires: expires, Days: int(expires.Sub(now).Hours() / 24), Hosts: certUse[c.ID],
		}
		if cert.Name == "" {
			cert.Name = strings.Join(c.DomainNames, ", ")
		}
		if !expires.IsZero() && cert.Days < warnDays {
			out.Expiring++
		}
		out.Certificates = append(out.Certificates, cert)
	}
	sort.SliceStable(out.Certificates, func(i, j int) bool {
		return out.Certificates[i].Expires.Before(out.Certificates[j].Expires)
	})
	sort.SliceStable(out.Hosts, func(i, j int) bool {
		return firstDomain(out.Hosts[i]) < firstDomain(out.Hosts[j])
	})
	return out, nil
}

func firstDomain(h ProxyHost) string {
	if len(h.Domains) == 0 {
		return ""
	}
	return h.Domains[0]
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || t == "true"
	}
	return v == nil // missing means enabled
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}
