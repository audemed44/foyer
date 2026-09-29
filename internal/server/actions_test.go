package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/monitor"
)

func TestWidgetAction(t *testing.T) {
	var deploys, polls atomic.Int32
	var auth string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/foyer/widget":
			io.WriteString(w, `{"version":1,"items":[
				{"title":"main-stack","action":{"label":"Deploy","url":"/api/foyer/deploy/main-stack","confirm":"Deploy?"}},
				{"title":"evil","action":{"label":"x","url":"https://elsewhere.example/api"}}]}`)
		case r.Method == "POST" && r.URL.Path == "/api/foyer/deploy/main-stack":
			deploys.Add(1)
			auth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"message":"Deploying main-stack…","status_url":"/api/foyer/jobs/1","url":"/jobs/1"}`)
		case r.URL.Path == "/api/foyer/jobs/1":
			if polls.Add(1) == 1 {
				io.WriteString(w, `{"state":"running","message":"Deploying main-stack…"}`)
				return
			}
			io.WriteString(w, `{"state":"done","message":"main-stack: Recreated foyer","url":"/jobs/1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer app.Close()

	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Infra", Services: []config.Service{{
		Name: "Hoist", URL: "https://hoist.example.com",
		Widget: config.Widget{"type": "app", "url": app.URL + "/api/foyer/widget", "key": "k3y"},
	}}}}
	store.WriteInitial(cfg)
	h := New(store, monitor.New(store, nil, "/proc", "/sys"), nil, dir, nil).Handler()

	// The unsafe action was dropped from the widget data.
	var widget struct {
		Items []struct {
			Action *struct{ URL string } `json:"action"`
		} `json:"items"`
	}
	json.Unmarshal(do(h, "GET", "/api/widgets/hoist", "").Body.Bytes(), &widget)
	if len(widget.Items) != 2 || widget.Items[0].Action == nil || widget.Items[1].Action != nil {
		t.Fatalf("widget: %+v", widget)
	}

	run := func(u string, headers ...string) *httptest.ResponseRecorder {
		return post(h, "/api/widgets/hoist/action", strings.NewReader(`{"url":"`+u+`"}`), "application/json", headers...)
	}
	if rec := run("/api/foyer/deploy/other"); rec.Code != http.StatusBadRequest {
		t.Errorf("unlisted action: %d", rec.Code)
	}
	if rec := run("/api/foyer/deploy/main-stack", "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if deploys.Load() != 0 {
		t.Fatal("the app was called for a refused action")
	}

	rec := run("/api/foyer/deploy/main-stack")
	var started actionResponse
	json.Unmarshal(rec.Body.Bytes(), &started)
	if rec.Code != 200 || started.State != "running" || started.Status != "/api/foyer/jobs/1" ||
		started.URL != "https://hoist.example.com/jobs/1" || auth != "Bearer k3y" {
		t.Fatalf("run: %d %+v (auth %q)", rec.Code, started, auth)
	}

	status := func(s string) (int, actionResponse) {
		rec := do(h, "GET", "/api/widgets/hoist/action?status="+url.QueryEscape(s), "")
		var res actionResponse
		json.Unmarshal(rec.Body.Bytes(), &res)
		return rec.Code, res
	}
	if code, _ := status("/api/foyer/jobs/2"); code != http.StatusNotFound {
		t.Errorf("status the app never gave: %d", code)
	}
	if _, res := status(started.Status); res.State != "running" {
		t.Errorf("first poll: %+v", res)
	}
	if _, res := status(started.Status); res.State != "done" || res.Message != "main-stack: Recreated foyer" {
		t.Errorf("second poll: %+v", res)
	}
}
