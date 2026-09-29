// Package alerts sends notifications through Apprise when something goes
// wrong and again when it recovers. It only notifies on changes: a problem
// alerts once, however long it lasts. The set of open problems is saved, so
// a restart neither repeats alerts nor loses recoveries.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

// Problem is something wrong right now.
type Problem struct {
	Key   string // stable identity, e.g. "service:jellyfin"
	Title string // "Jellyfin is down"
	Body  string
	// Level is failure or warning (Apprise's notification types).
	Level string
	// After is how many checks in a row must see it before alerting.
	After int
	// Recovered is the title sent when it clears ("Jellyfin is back up").
	Recovered string
}

type Event struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"` // failure | warning | success
	Title string    `json:"title"`
	Body  string    `json:"body,omitempty"`
	// Error is set when Apprise couldn't be reached.
	Error string `json:"error,omitempty"`
}

type open struct {
	Title     string    `json:"title"`
	Level     string    `json:"level"`
	Recovered string    `json:"recovered"`
	Since     time.Time `json:"since"`
}

type state struct {
	Open    map[string]open `json:"open"`
	History []Event         `json:"history"`
}

const historyLen = 50

type Engine struct {
	path string
	send func(ctx context.Context, cfg config.Alerts, e Event) error
	now  func() time.Time

	mu      sync.Mutex
	st      state
	loaded  bool
	pending map[string]int // consecutive sightings not yet alerted
}

// New keeps its state in path (e.g. /config/alerts.json).
func New(path string) *Engine {
	return &Engine{path: path, send: Send, now: time.Now, pending: map[string]int{}}
}

func (e *Engine) load() {
	if e.loaded {
		return
	}
	e.loaded = true
	e.st = state{Open: map[string]open{}, History: []Event{}}
	data, err := os.ReadFile(e.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &e.st); err != nil {
		slog.Warn("alerts: ignoring unreadable state", "err", err)
		e.st = state{Open: map[string]open{}, History: []Event{}}
	}
	if e.st.Open == nil {
		e.st.Open = map[string]open{}
	}
}

func (e *Engine) save() {
	data, err := json.MarshalIndent(e.st, "", "  ")
	if err == nil {
		if err = os.WriteFile(e.path+".tmp", data, 0o644); err == nil {
			err = os.Rename(e.path+".tmp", e.path)
		}
	}
	if err != nil {
		slog.Warn("alerts: could not save state", "err", err)
	}
}

// Evaluate compares what's wrong now with what was open, and notifies about
// new problems and recoveries. scope limits recoveries to the kinds of
// problem this check looked at (key prefixes), so a check that runs less
// often doesn't "recover" another check's problems.
func (e *Engine) Evaluate(ctx context.Context, cfg config.Alerts, scope []string, problems []Problem) {
	e.mu.Lock()
	e.load()
	var events []Event
	now := e.now()
	seen := map[string]bool{}
	for _, p := range problems {
		seen[p.Key] = true
		if _, ok := e.st.Open[p.Key]; ok {
			continue
		}
		e.pending[p.Key]++
		if e.pending[p.Key] < max(1, p.After) {
			continue
		}
		delete(e.pending, p.Key)
		e.st.Open[p.Key] = open{Title: p.Title, Level: p.Level, Recovered: p.Recovered, Since: now}
		events = append(events, Event{At: now, Level: p.Level, Title: p.Title, Body: p.Body})
	}
	inScope := func(key string) bool {
		for _, prefix := range scope {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		return false
	}
	for key := range e.pending {
		if !seen[key] && inScope(key) {
			delete(e.pending, key)
		}
	}
	var cleared []string
	for key := range e.st.Open {
		if !seen[key] && inScope(key) {
			cleared = append(cleared, key)
		}
	}
	sort.Strings(cleared)
	for _, key := range cleared {
		o := e.st.Open[key]
		delete(e.st.Open, key)
		title := o.Recovered
		if title == "" {
			title = "Resolved: " + o.Title
		}
		events = append(events, Event{At: now, Level: "success", Title: title,
			Body: "After " + duration(now.Sub(o.Since)) + "."})
	}
	e.mu.Unlock()
	if len(events) == 0 {
		return
	}

	for i := range events {
		if cfg.AppriseURL != "" {
			if err := e.send(ctx, cfg, events[i]); err != nil {
				events[i].Error = err.Error()
				slog.Warn("alerts: could not notify", "title", events[i].Title, "err", err)
			}
		}
	}
	e.mu.Lock()
	e.st.History = append(events, e.st.History...)
	e.st.History = e.st.History[:min(len(e.st.History), historyLen)]
	e.save()
	e.mu.Unlock()
}

// Snapshot returns the open problems and recent events, newest first.
func (e *Engine) Snapshot() (openNow []Event, history []Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.load()
	openNow = []Event{}
	for _, o := range e.st.Open {
		openNow = append(openNow, Event{At: o.Since, Level: o.Level, Title: o.Title})
	}
	sort.Slice(openNow, func(i, j int) bool { return openNow[i].At.After(openNow[j].At) })
	return openNow, append([]Event{}, e.st.History...)
}

func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1f h", d.Hours())
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

var client = &http.Client{Timeout: 15 * time.Second}

// Send posts a notification to an Apprise API endpoint: a stateful
// /notify/<key> URL, which delivers to the services saved under that key.
func Send(ctx context.Context, cfg config.Alerts, e Event) error {
	body := map[string]string{"title": e.Title, "body": e.Body, "type": e.Level}
	if body["body"] == "" {
		body["body"] = e.Title
	}
	if cfg.Tag != "" {
		body["tag"] = cfg.Tag
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.ExpandEnv(cfg.AppriseURL), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("invalid Apprise URL")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		// Apprise's answer when the key has no services saved.
		return fmt.Errorf("Apprise has no notification services saved under this key")
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("Apprise answered HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

// Forget drops open problems with these key prefixes without notifying,
// for kinds of alert that were turned off.
func (e *Engine) Forget(prefixes ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.load()
	changed := false
	for key := range e.st.Open {
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				delete(e.st.Open, key)
				changed = true
			}
		}
	}
	for key := range e.pending {
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				delete(e.pending, key)
			}
		}
	}
	if changed {
		e.save()
	}
}
