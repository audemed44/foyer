package widgets

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/audemed44/foyer/internal/config"
)

func appServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case WellKnownPath:
			if r.Header.Get("Authorization") == "Bearer wrong" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"version":1,
				"stats":[{"label":"Read","value":"12","unit":"/52","tone":"good"},{"label":"x","value":"1","tone":"neon"},
					{"label":"3","value":"3"},{"label":"4","value":"4"},{"label":"5","value":"5"},{"label":"6","value":"6"},{"label":"7","value":"7"}],
				"progress":[{"label":"Goal","value":12,"max":52,"caption":"behind"}],
				"items_title":"Reading","items_layout":"covers",
				"items":[
					{"title":"Book","image":"/api/cover/1","url":"/books/1","progress":140},
					{"title":"Evil","image":"//evil.example/x.png","url":"javascript:alert(1)","progress":-5},
					{"title":"Remote","image":"https://cdn.example/c.jpg"}
				]}`)
		case "/api/cover/1":
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "JPEG")
		case "/api/cover/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, "<svg/>")
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>")
		case "/old":
			fmt.Fprint(w, `{"version":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAppWidgetSanitizes(t *testing.T) {
	srv := appServer(t)
	got, err := app(context.Background(), config.Widget{"url": srv.URL + WellKnownPath})
	if err != nil {
		t.Fatal(err)
	}
	w := got.(AppWidget)
	if len(w.Stats) != 6 || w.Stats[0].Unit != "/52" || w.Stats[0].Tone != "good" || w.Stats[1].Tone != "" {
		t.Fatalf("stats: %+v", w.Stats)
	}
	if w.ItemsLayout != "covers" || len(w.Progress) != 1 || len(w.Items) != 3 {
		t.Fatalf("widget: %+v", w)
	}
	book, evil, remote := w.Items[0], w.Items[1], w.Items[2]
	if *book.Progress != 100 || book.Image != "/api/cover/1" || book.URL != "/books/1" {
		t.Fatalf("book: %+v", book)
	}
	if *evil.Progress != 0 || evil.Image != "" || evil.URL != "" {
		t.Fatalf("unsafe item should be cleaned: %+v", evil)
	}
	if remote.Image != "https://cdn.example/c.jpg" {
		t.Fatalf("https images are kept: %+v", remote)
	}

	if _, err := app(context.Background(), config.Widget{"url": srv.URL + "/old"}); err == nil ||
		!strings.Contains(err.Error(), "version 2") {
		t.Fatalf("version check: %v", err)
	}
	if _, err := app(context.Background(), config.Widget{"url": srv.URL + WellKnownPath, "key": "wrong"}); err == nil {
		t.Fatal("the key should be sent as a bearer token")
	}
}

func TestProxyImage(t *testing.T) {
	srv := appServer(t)
	w := config.Widget{"type": "app", "url": srv.URL + WellKnownPath}

	rec := httptest.NewRecorder()
	if err := ProxyImage(context.Background(), w, "/api/cover/1", rec); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "JPEG" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %q %v", rec.Body, rec.Header())
	}

	for _, path := range []string{"//evil.example/x.png", "https://evil.example/x.png", "cover", `/\evil`, "/api/cover/svg", "/page", "/missing"} {
		if err := ProxyImage(context.Background(), w, path, httptest.NewRecorder()); err == nil {
			t.Errorf("%q should be refused", path)
		}
	}
	if err := ProxyImage(context.Background(), config.Widget{"type": "speedtest", "url": srv.URL}, "/api/cover/1", httptest.NewRecorder()); err == nil {
		t.Error("only app widgets proxy images")
	}
}

func TestProbe(t *testing.T) {
	srv := appServer(t)
	if url, ok := Probe(context.Background(), srv.URL+"/some/page?x=1"); !ok || url != srv.URL+WellKnownPath {
		t.Fatalf("probe = %q %v", url, ok)
	}
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()
	if _, ok := Probe(context.Background(), plain.URL); ok {
		t.Fatal("an app without the endpoint shouldn't match")
	}
	if _, ok := Probe(context.Background(), "not a url"); ok {
		t.Fatal("bad base")
	}
}
