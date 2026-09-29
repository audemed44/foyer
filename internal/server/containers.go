package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/discover"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/widgets"
)

type containerInfo struct {
	docker.Container
	Stats *docker.Stats `json:"stats,omitempty"`
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker socket not mounted")
		return
	}
	list, err := s.docker.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach Docker: "+err.Error())
		return
	}
	var running []docker.Container
	for _, c := range list {
		if c.State == "running" {
			running = append(running, c)
		}
	}
	stats := s.docker.Stats(r.Context(), running)
	out := make([]containerInfo, len(list))
	for i, c := range list {
		out[i] = containerInfo{Container: c}
		if st, ok := stats[c.ID]; ok {
			out[i].Stats = &st
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// containerLogs streams a container's logs as Server-Sent Events. Each event
// is one line; its id is the line's timestamp, so a reconnecting EventSource
// (which sends Last-Event-ID) resumes where it left off.
func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker socket not mounted")
		return
	}
	ct, err := s.docker.Find(r.Context(), r.PathValue("name"))
	if errors.Is(err, docker.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such container")
		return
	} else if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach Docker: "+err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	tail, err := strconv.Atoi(r.URL.Query().Get("tail"))
	if err != nil {
		tail = 500
	}
	tail = max(0, min(tail, 5000))
	since := r.Header.Get("Last-Event-ID")
	if _, err := time.Parse(time.RFC3339Nano, since); err != nil {
		since = ""
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // don't let nginx buffer the stream
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	// Batch writes: flush when idle for a moment rather than per line, so a
	// burst of history doesn't turn into thousands of tiny writes.
	lines := make(chan docker.LogLine, 256)
	done := make(chan error, 1)
	go func() {
		done <- s.docker.Logs(r.Context(), ct, tail, since, true, func(l docker.LogLine) error {
			select {
			case lines <- l:
				return nil
			case <-r.Context().Done():
				return r.Context().Err()
			}
		})
		close(lines)
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	pending := false
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				err := <-done
				if err != nil {
					fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
				}
				// The container stopped; tell the client not to reconnect blindly.
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(l)
			if l.Time != "" {
				fmt.Fprintf(w, "id: %s\n", l.Time)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			pending = true
		case <-ticker.C:
			if pending {
				flusher.Flush()
				pending = false
			}
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// discoverResponse lists containers to add and existing services whose app
// serves a Foyer widget they don't show yet.
type discoverResponse struct {
	Containers []discover.Suggestion `json:"containers"`
	Widgets    []widgetSuggestion    `json:"widgets"`
}

type widgetSuggestion struct {
	Service string        `json:"service"` // service id
	Name    string        `json:"name"`
	Widget  config.Widget `json:"widget"`
}

// discoverServices suggests running containers that aren't on the dashboard,
// and probes apps for the Foyer widget endpoint.
func (s *Server) discoverServices(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Config()
	resp := discoverResponse{Containers: []discover.Suggestion{}, Widgets: []widgetSuggestion{}}
	if s.docker != nil {
		list, err := s.docker.List(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "could not reach Docker: "+err.Error())
			return
		}
		resp.Containers = discover.Suggest(cfg, list)
		// A proxy host in NPM beats a guessed link.
		links := s.npmLinks(r.Context(), cfg, list)
		for i := range resp.Containers {
			c := &resp.Containers[i]
			if link, ok := links[c.Container]; ok && (c.URL == "" || c.URLGuessed) {
				c.URL, c.URLGuessed, c.URLFromNPM = link, false, true
			}
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	probe := func(base string, found func(url string)) {
		if base == "" {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if url, ok := widgets.Probe(r.Context(), config.ExpandEnv(base)); ok {
				mu.Lock()
				found(url)
				mu.Unlock()
			}
		}()
	}
	for i := range resp.Containers {
		c := &resp.Containers[i]
		probe(c.Ping, func(url string) { c.Widget = config.Widget{"type": "app", "url": url} })
	}
	for _, svc := range cfg.Services() {
		if svc.Widget != nil {
			continue
		}
		probe(svc.Ping, func(url string) {
			resp.Widgets = append(resp.Widgets, widgetSuggestion{
				Service: svc.ID, Name: svc.Name, Widget: config.Widget{"type": "app", "url": url},
			})
		})
	}
	wg.Wait()
	sort.Slice(resp.Widgets, func(i, j int) bool { return resp.Widgets[i].Name < resp.Widgets[j].Name })
	writeJSON(w, http.StatusOK, resp)
}
