package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/monitor"
)

// Lookout's checks become the status of the services they watch, and
// Gatehouse's sleeping apps aren't "exited".
func TestStatusFromLookoutAndGatehouse(t *testing.T) {
	dir, _ := os.MkdirTemp("", "dk")
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	dmux := http.NewServeMux()
	dmux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"Id":"1","Names":["/convertx"],"State":"exited","Status":"Exited (0) 5 minutes ago"},
			{"Id":"2","Names":["/books"],"State":"running","Status":"Up"},
			{"Id":"3","Names":["/broken"],"State":"exited","Status":"Exited (1) 1 minute ago"}]`)
	})
	dsrv := &http.Server{Handler: dmux}
	go dsrv.Serve(ln)
	t.Cleanup(func() { dsrv.Close() })
	dock := docker.New(sock)

	pinged := 0
	pingTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { pinged++ }))
	t.Cleanup(pingTarget.Close)
	lookout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/foyer/widget":
			fmt.Fprint(w, `{"version":1,"stats":[],"items":[]}`)
		case "/api/checks":
			fmt.Fprintf(w, `[{"check":{"name":"Books","type":"http","target":"https://books.example.com/"},"status":"up","state":{"message":"HTTP 200","latency":12.4}},
				{"check":{"name":"Watched","type":"http","target":%q},"status":"down","state":{"message":"HTTP 502"}}]`, pingTarget.URL)
		}
	}))
	t.Cleanup(lookout.Close)
	gatehouse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/foyer/widget":
			fmt.Fprint(w, `{"version":1,"stats":[],"items":[]}`)
		case "/api/discovery":
			fmt.Fprint(w, `{"hosts":[{"domains":["convert.example.com"],"enabled":true,"container":"convertx","idle_stop":"30m","state":"sleeping"}],"redirects":[],"certificates":[]}`)
		}
	}))
	t.Cleanup(gatehouse.Close)

	store := config.NewStore(filepath.Join(t.TempDir(), "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Apps", Services: []config.Service{
		{ID: "books", Name: "Books", URL: "https://books.example.com", Container: "books"},
		{ID: "convertx", Name: "Convertx", URL: "https://convert.example.com", Container: "convertx"},
		{ID: "broken", Name: "Broken", Container: "broken"},
		{ID: "watched", Name: "Watched", Ping: pingTarget.URL},
		{ID: "lookout", Name: "Lookout", Widget: config.Widget{"type": "lookout", "url": lookout.URL, "key": "k"}},
		{ID: "gatehouse", Name: "Gatehouse", Widget: config.Widget{"type": "gatehouse", "url": gatehouse.URL, "key": "k"}},
	}}}
	if err := store.WriteInitial(cfg); err != nil {
		t.Fatal(err)
	}
	mon := monitor.New(store, dock, "/proc", "/sys")
	s := New(store, mon, dock, t.TempDir(), nil)
	mon.Watched = s.WatchedByLookout
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mon.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for mon.Status()["books"].Container == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	var got map[string]monitor.ServiceStatus
	json.Unmarshal(do(s.Handler(), "GET", "/api/status", "").Body.Bytes(), &got)
	if c := got["books"].Check; c == nil || c.State != "up" || c.LatencyMS != 12 {
		t.Fatalf("books: %+v", got["books"])
	}
	if c := got["convertx"].Container; c == nil || c.State != "asleep" {
		t.Fatalf("convertx: %+v", got["convertx"])
	}
	if c := got["broken"].Container; c == nil || c.State != "exited" {
		t.Fatalf("broken should still be exited: %+v", got["broken"])
	}
	if st := got["watched"]; st.Check == nil || st.Check.State != "down" || st.Ping != nil {
		t.Fatalf("watched: %+v", st)
	}
	if pinged != 0 {
		t.Fatalf("Foyer pinged a service Lookout watches %d times", pinged)
	}
}
