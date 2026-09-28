package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/monitor"
)

func setup(t *testing.T) (http.Handler, *config.Store) {
	t.Helper()
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Net", Services: []config.Service{{
		Name: "Speed", Ping: "http://internal:80",
		Widget: config.Widget{"type": "speedtest", "url": "http://internal", "key": "s3cret"},
	}}}}
	if err := store.WriteInitial(cfg); err != nil {
		t.Fatal(err)
	}
	mon := monitor.New(store, nil, "/proc", "/sys")
	web := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/a.js": {Data: []byte("js")}}
	return New(store, mon, nil, dir, web).Handler(), store
}

func do(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicConfigHidesSecrets(t *testing.T) {
	h, _ := setup(t)
	rec := do(h, "GET", "/api/config", "")
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"s3cret", "internal"} {
		if strings.Contains(body, leak) {
			t.Fatalf("public config leaks %q: %s", leak, body)
		}
	}
	var resp configResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.WidgetTypes) == 0 {
		t.Fatalf("response: %v %+v", err, resp)
	}
}

func TestEditRoundTripKeepsSecrets(t *testing.T) {
	h, store := setup(t)
	rec := do(h, "GET", "/api/config/edit", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "s3cret") || !strings.Contains(rec.Body.String(), config.SecretMask) {
		t.Fatalf("editable config: %d %s", rec.Code, rec.Body)
	}

	edited := strings.Replace(rec.Body.String(), `"title":"Foyer"`, `"title":"Mine"`, 1)
	rec = do(h, "PUT", "/api/config", edited)
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	cfg := store.Config()
	if cfg.Title != "Mine" || cfg.Groups[0].Services[0].Widget["key"] != "s3cret" {
		t.Fatalf("saved: %q %v", cfg.Title, cfg.Groups[0].Services[0].Widget)
	}

	bad := strings.Replace(edited, `"accent":"#2563ff"`, `"accent":"red"`, 1)
	if rec := do(h, "PUT", "/api/config", bad); rec.Code != 422 {
		t.Fatalf("invalid config: %d", rec.Code)
	}
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(edited))
	req.Header.Set("Content-Type", "text/plain")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON save: %d", res.Code)
	}
}

func TestSPAFallbackAndWidget404(t *testing.T) {
	h, _ := setup(t)
	if rec := do(h, "GET", "/some/route", ""); !strings.Contains(rec.Body.String(), "app") {
		t.Fatal("unknown routes should serve index.html")
	}
	if rec := do(h, "GET", "/assets/a.js", ""); !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("assets should be cached")
	}
	if rec := do(h, "GET", "/api/widgets/nope", ""); rec.Code != 404 {
		t.Fatalf("widget: %d", rec.Code)
	}
	if rec := do(h, "GET", "/icons/", ""); rec.Code != 404 {
		t.Fatalf("directory listing: %d", rec.Code)
	}
}
