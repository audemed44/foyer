package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/widgets"
)

// actionStatuses remembers the status URLs apps handed out, so the browser
// can only poll addresses an app gave for an action it started.
type actionStatuses struct {
	mu   sync.Mutex
	urls map[string]time.Time // service id + "\x00" + status url → expiry
}

const actionStatusTTL = 2 * time.Hour

func newActionStatuses() *actionStatuses {
	return &actionStatuses{urls: map[string]time.Time{}}
}

func (a *actionStatuses) add(service, url string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, exp := range a.urls {
		if now.After(exp) {
			delete(a.urls, k)
		}
	}
	a.urls[service+"\x00"+url] = now.Add(actionStatusTTL)
}

func (a *actionStatuses) known(service, url string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.urls[service+"\x00"+url]
	return ok && time.Now().Before(exp)
}

// actionResponse is what the browser gets: the app's answer, with its link
// pointing at the app's public page.
type actionResponse struct {
	State   string `json:"state"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
	Status  string `json:"status,omitempty"` // pass back to poll
}

func (s *Server) actionResponse(serviceURL string, res widgets.ActionResult) actionResponse {
	return actionResponse{State: res.State, Message: res.Message, URL: publicLink(res.URL, serviceURL), Status: res.StatusURL}
}

// runWidgetAction runs an action an app's widget lists on one of its items,
// e.g. Hoist's Deploy. The request goes to the app's own address with the
// widget's key; the browser only names which listed action.
func (s *Server) runWidgetAction(w http.ResponseWriter, r *http.Request) {
	if crossSite(r) {
		writeError(w, http.StatusForbidden, "cross-site request")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"url": action url}`)
		return
	}
	svc := s.store.Config().Service(r.PathValue("id"))
	if svc == nil || svc.Widget == nil || svc.Widget.Type() != "app" {
		writeError(w, http.StatusNotFound, "no such widget")
		return
	}
	data, err := s.widgets.Fetch(r.Context(), svc.ID, svc.Widget)
	app, ok := data.(widgets.AppWidget)
	if err != nil || !ok || !app.HasAction(body.URL) {
		writeError(w, http.StatusBadRequest, svc.Name+" doesn't offer that action")
		return
	}
	res, err := widgets.RunAction(r.Context(), svc.Widget, body.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, svc.Name+": "+err.Error())
		return
	}
	if res.StatusURL != "" {
		s.actions.add(svc.ID, res.StatusURL)
	}
	s.widgets.Invalidate(svc.ID)
	if res.Message == "" {
		res.Message = "Done"
	}
	writeJSON(w, http.StatusOK, s.actionResponse(svc.URL, res))
}

// widgetActionStatus polls a running action (?status=<the status it gave>).
func (s *Server) widgetActionStatus(w http.ResponseWriter, r *http.Request) {
	svc := s.store.Config().Service(r.PathValue("id"))
	status := r.URL.Query().Get("status")
	if svc == nil || svc.Widget == nil || !s.actions.known(svc.ID, status) {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	res, err := widgets.ActionStatus(r.Context(), svc.Widget, status)
	if err != nil {
		writeError(w, http.StatusBadGateway, svc.Name+": "+err.Error())
		return
	}
	if res.StatusURL == "" {
		res.StatusURL = status
	}
	if res.State != "running" {
		// The card should show what the action changed.
		s.widgets.Invalidate(svc.ID)
	}
	writeJSON(w, http.StatusOK, s.actionResponse(svc.URL, res))
}
