// Package widgets fetches the data shown inside service widget cards. The
// server talks to the upstream services so their URLs and keys stay private.
package widgets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

// cacheTTL keeps a dashboard with several open tabs from hammering upstreams.
const cacheTTL = 45 * time.Second

type fetcher func(ctx context.Context, w config.Widget) (any, error)

var fetchers = map[string]fetcher{
	"uptimekuma": uptimeKuma,
	"speedtest":  speedtest,
	"calendar":   calendar,
	"app":        app,
	"kopia":      kopia,
	"syncthing":  syncthing,
	"komodo":     komodo,
	"npm":        npm,
	"gatehouse":  gatehouse,
	"lookout":    lookout,
	"keep":       keep,
}

// Types lists the supported widget types, for the editor.
func Types() []string {
	return []string{"app", "uptimekuma", "speedtest", "calendar", "kopia", "syncthing", "komodo", "npm", "gatehouse", "lookout", "keep"}
}

var ErrUnknownType = errors.New("unknown widget type")

type cached struct {
	data    any
	err     error
	at      time.Time
	version string
}

type Service struct {
	mu    sync.Mutex
	cache map[string]cached
	now   func() time.Time
}

func NewService() *Service {
	return &Service{cache: map[string]cached{}, now: time.Now}
}

// Fetch returns the widget data for a service, from cache when fresh.
func (s *Service) Fetch(ctx context.Context, serviceID string, w config.Widget) (any, error) {
	f, ok := fetchers[w.Type()]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownType, w.Type())
	}
	// A config edit invalidates the entry: the key includes the widget settings.
	raw, _ := json.Marshal(w)
	version := string(raw)
	s.mu.Lock()
	entry, hit := s.cache[serviceID]
	s.mu.Unlock()
	if hit && entry.version == version && s.now().Sub(entry.at) < cacheTTL {
		return entry.data, entry.err
	}
	expanded := config.Widget{}
	for k, v := range w {
		if str, ok := v.(string); ok {
			v = config.ExpandEnv(str)
		}
		expanded[k] = v
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := f(ctx, expanded)
	s.mu.Lock()
	s.cache[serviceID] = cached{data, err, s.now(), version}
	s.mu.Unlock()
	return data, err
}

// Invalidate drops a service's cached data, e.g. after one of its actions
// changed what the widget shows.
func (s *Service) Invalidate(serviceID string) {
	s.mu.Lock()
	delete(s.cache, serviceID)
	s.mu.Unlock()
}

var client = &http.Client{Timeout: 10 * time.Second}

// getJSON fetches url into out. headers are optional name/value pairs.
func getJSON(ctx context.Context, url string, out any, headers ...string) error {
	return doJSON(ctx, http.MethodGet, url, nil, out, headers...)
}

// postJSON sends body as JSON and decodes the answer into out.
func postJSON(ctx context.Context, url string, body, out any, headers ...string) error {
	return doJSON(ctx, http.MethodPost, url, body, out, headers...)
}

func doJSON(ctx context.Context, method, url string, body, out any, headers ...string) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s rejected the credentials (HTTP %d)", req.URL.Host, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered HTTP %d", req.URL.Host, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("unexpected response from %s", req.URL.Host)
	}
	return nil
}

func required(w config.Widget, keys ...string) error {
	for _, k := range keys {
		if strings.TrimSpace(w.String(k)) == "" {
			return fmt.Errorf("widget needs %q", k)
		}
	}
	return nil
}

func join(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
