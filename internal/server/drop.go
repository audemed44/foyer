package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/audemed44/foyer/internal/drop"
	"github.com/audemed44/foyer/internal/widgets"
)

type dropResponse struct {
	Items   []drop.Item `json:"items"`
	MaxFile int64       `json:"max_file"`
	Usage   int64       `json:"usage"`
}

func (s *Server) listDrop(w http.ResponseWriter, _ *http.Request) {
	items, err := s.drop.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dropResponse{Items: items, MaxFile: s.drop.MaxFile(), Usage: s.drop.Usage()})
}

// crossSite rejects form posts from other sites. Foyer has no login, so this
// is what stops a random web page from filling the inbox.
func crossSite(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "cross-site"
}

// readDrop saves a multipart form (fields title, text, url and any number of
// files), streaming files straight to disk.
func (s *Server) readDrop(r *http.Request) ([]drop.Item, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("expected a multipart form")
	}
	fields := map[string]string{}
	var items []drop.Item
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return items, fmt.Errorf("upload interrupted")
		}
		if part.FileName() == "" {
			value, _ := io.ReadAll(io.LimitReader(part, drop.MaxText+1))
			fields[part.FormName()] = string(value)
			continue
		}
		it, err := s.drop.AddFile(part.FileName(), part.Header.Get("Content-Type"), part)
		if err != nil {
			return items, err
		}
		items = append(items, it)
	}
	text, link, title := fields["text"], fields["url"], fields["title"]
	if strings.TrimSpace(text+link) == "" {
		// A shared file often comes with its name as the title; that's no note.
		if len(items) == 0 && strings.TrimSpace(title) != "" {
			text, title = title, ""
		}
	}
	if strings.TrimSpace(text+link) != "" {
		it, err := s.drop.AddText(text, link, title)
		if err != nil {
			return items, err
		}
		if it.Kind == "link" && it.Title == "" {
			go func() {
				if t := drop.FetchTitle(context.Background(), it.URL); t != "" {
					_ = s.drop.SetTitle(it.ID, t)
				}
			}()
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil, drop.ErrEmpty
	}
	return items, nil
}

func dropStatus(err error) int {
	switch {
	case errors.Is(err, drop.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, drop.ErrNotFound):
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (s *Server) postDrop(w http.ResponseWriter, r *http.Request) {
	if crossSite(r) {
		writeError(w, http.StatusForbidden, "cross-site request")
		return
	}
	items, err := s.readDrop(r)
	if err != nil {
		writeError(w, dropStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, items)
}

// share receives the installed app's share target (a regular form post) and
// sends the browser on to the inbox.
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	if crossSite(r) {
		http.Error(w, "cross-site request", http.StatusForbidden)
		return
	}
	if _, err := s.readDrop(r); err != nil {
		http.Redirect(w, r, "/#/drop?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/#/drop", http.StatusSeeOther)
}

func (s *Server) deleteDrop(w http.ResponseWriter, r *http.Request) {
	if err := s.drop.Delete(r.PathValue("id")); err != nil {
		writeError(w, dropStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inlineTypes are shown in the browser; everything else is downloaded, so
// an uploaded HTML or SVG file can never run as part of the dashboard.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
	"video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/mp4": true,
	"application/pdf": true, "text/plain": true,
}

func (s *Server) dropFile(w http.ResponseWriter, r *http.Request) {
	it, f, err := s.drop.Open(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	defer f.Close()
	ct := it.File.Type
	disposition := "attachment"
	if inlineTypes[ct] && r.URL.Query().Get("download") == "" {
		disposition = "inline"
	}
	if !inlineTypes[ct] {
		ct = "application/octet-stream"
	} else if ct == "text/plain" {
		ct = "text/plain; charset=utf-8"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": it.File.Name}))
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// ServeContent handles range requests, so video scrubbing works.
	http.ServeContent(w, r, "", info.ModTime(), f)
}

type dropTarget struct {
	Service string   `json:"service"`
	Name    string   `json:"name"`
	Icon    string   `json:"icon,omitempty"`
	Label   string   `json:"label"`
	Types   []string `json:"types"`
}

// accepting returns the apps whose widget says they take files, with the
// widget's accepts rule, keyed by service id.
func (s *Server) accepting(ctx context.Context) ([]dropTarget, map[string]*widgets.AppAccepts) {
	cfg := s.store.Config()
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		targets = []dropTarget{}
		rules   = map[string]*widgets.AppAccepts{}
	)
	for _, svc := range cfg.Services() {
		if svc.Widget == nil || !widgets.IsApp(svc.Widget) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := s.widgets.Fetch(ctx, svc.ID, svc.Widget)
			app, ok := widgets.AsApp(data)
			if err != nil || !ok || app.Accepts == nil {
				return
			}
			label := app.Accepts.Label
			if label == "" {
				label = "Send to " + svc.Name
			}
			mu.Lock()
			defer mu.Unlock()
			rules[svc.ID] = app.Accepts
			targets = append(targets, dropTarget{
				Service: svc.ID, Name: svc.Name, Icon: svc.Icon, Label: label, Types: app.Accepts.Types,
			})
		}()
	}
	wg.Wait()
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, rules
}

func (s *Server) dropTargets(w http.ResponseWriter, r *http.Request) {
	targets, _ := s.accepting(r.Context())
	writeJSON(w, http.StatusOK, targets)
}

// sendDrop uploads a file from the inbox to an app that accepts it.
func (s *Server) sendDrop(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "expected JSON")
		return
	}
	var body struct {
		Service string `json:"service"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "expected {\"service\": id}")
		return
	}
	svc := s.store.Config().Service(body.Service)
	if svc == nil || svc.Widget == nil {
		writeError(w, http.StatusNotFound, "no such service")
		return
	}
	_, rules := s.accepting(r.Context())
	rule := rules[svc.ID]
	it, f, err := s.drop.Open(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	defer f.Close()
	if !rule.Accepts(it.File.Name, it.File.Type) {
		writeError(w, http.StatusBadRequest, svc.Name+" doesn't take this kind of file")
		return
	}
	sent, err := widgets.SendFile(r.Context(), svc.Widget, rule, it.File.Name, it.File.Type, f)
	if err != nil {
		writeError(w, http.StatusBadGateway, svc.Name+": "+err.Error())
		return
	}
	resp := map[string]string{"message": sent.Message}
	if resp["message"] == "" {
		resp["message"] = "Sent to " + svc.Name
	}
	// A relative link opens the app's public page, like widget item links.
	if link := publicLink(sent.URL, svc.URL); link != "" {
		resp["url"] = link
	}
	writeJSON(w, http.StatusOK, resp)
}

// dropLimit is the largest file the inbox takes: FOYER_DROP_MAX_MB, default 512.
func dropLimit() int64 {
	mb, err := strconv.Atoi(os.Getenv("FOYER_DROP_MAX_MB"))
	if err != nil || mb <= 0 {
		mb = 512
	}
	return int64(mb) << 20
}

// publicLink resolves an app's link against the service's public address.
func publicLink(link, public string) string {
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	base, err := url.Parse(public)
	if err != nil || base.Host == "" {
		return ""
	}
	ref, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}
