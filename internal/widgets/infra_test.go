package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

func TestKopiaUsesSessionAndCSRFToken(t *testing.T) {
	recent := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "Kopia-Session-Cookie", Value: "sess"})
			fmt.Fprint(w, `<html><head><meta name="kopia-csrf-token" content="abc123" /></head></html>`)
		case "/api/v1/sources":
			c, err := r.Cookie("Kopia-Session-Cookie")
			if err != nil || c.Value != "sess" || r.Header.Get("X-Kopia-Csrf-Token") != "abc123" {
				http.Error(w, "Invalid or missing CSRF token.", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"sources":[
				{"source":{"host":"h","userName":"root","path":"/data/new"},"status":"IDLE",
				 "lastSnapshot":{"startTime":%q,"endTime":%q,"stats":{"totalSize":100,"fileCount":3,"errorCount":0}}},
				{"source":{"host":"h","userName":"root","path":"/data/old"},"status":"IDLE",
				 "lastSnapshot":{"startTime":%q,"endTime":%q,"stats":{"totalSize":50,"fileCount":1,"errorCount":0}}},
				{"source":{"host":"h","userName":"root","path":"/data/none"},"status":"IDLE"}]}`,
				recent, recent, old, old)
		}
	}))
	defer srv.Close()

	got, err := kopia(context.Background(), config.Widget{"url": srv.URL, "username": "admin", "password": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	d := got.(KopiaData)
	if d.TotalSize != 150 || d.Problems != 2 || len(d.Sources) != 3 {
		t.Fatalf("summary: %+v", d)
	}
	states := []string{d.Sources[0].State, d.Sources[1].State, d.Sources[2].State}
	if strings.Join(states, ",") != "never,stale,ok" {
		t.Fatalf("problems should sort first: %v", states)
	}

	_, err = kopia(context.Background(), config.Widget{"url": srv.URL, "username": "admin", "password": "nope"})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("bad password: %v", err)
	}
}

func TestKopiaOverdueSchedule(t *testing.T) {
	now := time.Now()
	last := now.Add(-3 * time.Hour)
	next := now.Add(-2 * time.Hour)
	s := KopiaSource{Last: &last, Next: &next}
	if got := kopiaState(s, now, 48); got != "stale" {
		t.Fatalf("missed schedule: %s", got)
	}
	s.Next = nil
	s.Errors = 2
	if got := kopiaState(s, now, 48); got != "errors" {
		t.Fatalf("errors: %s", got)
	}
}

func TestSyncthing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/rest/system/status":
			fmt.Fprint(w, `{"myID":"ME"}`)
		case "/rest/config/folders":
			fmt.Fprint(w, `[{"id":"a","label":"Photos","devices":[{"deviceID":"ME"},{"deviceID":"PHONE"}]},
				{"id":"b","label":"Books","devices":[{"deviceID":"ME"}]},
				{"id":"c","label":"","paused":true}]`)
		case "/rest/config/devices":
			fmt.Fprint(w, `[{"deviceID":"ME","name":"server"},{"deviceID":"PHONE","name":"Phone"},{"deviceID":"LAPTOP-XYZ","name":""}]`)
		case "/rest/system/connections":
			fmt.Fprint(w, `{"connections":{"PHONE":{"connected":true},"LAPTOP-XYZ":{"connected":false}}}`)
		case "/rest/stats/device":
			fmt.Fprint(w, `{"PHONE":{"lastSeen":"2026-09-29T10:00:00Z"},"LAPTOP-XYZ":{"lastSeen":"1970-01-01T00:00:00Z"}}`)
		case "/rest/db/status":
			if r.URL.Query().Get("folder") == "a" {
				fmt.Fprint(w, `{"state":"syncing","globalBytes":200,"needBytes":50,"needTotalItems":4}`)
			} else {
				fmt.Fprint(w, `{"state":"error","error":"folder marker missing"}`)
			}
		}
	}))
	defer srv.Close()
	got, err := syncthing(context.Background(), config.Widget{"url": srv.URL, "key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	d := got.(SyncthingData)
	if len(d.Folders) != 3 || d.Problems != 1 || d.Connected != 1 || len(d.Devices) != 2 {
		t.Fatalf("summary: %+v", d)
	}
	byLabel := map[string]SyncFolder{}
	for _, f := range d.Folders {
		byLabel[f.Label] = f
	}
	if f := byLabel["Photos"]; f.Completion != 75 || f.Devices != 1 {
		t.Errorf("photos: %+v", f)
	}
	if f := byLabel["c"]; f.State != "paused" {
		t.Errorf("paused folder: %+v", f)
	}
	if d.Devices[1].Name != "LAPTOP" || d.Devices[1].LastSeen != nil {
		t.Errorf("never-seen device: %+v", d.Devices[1])
	}
}

func TestNPMLogsInOnceAndReadsCertificates(t *testing.T) {
	logins := 0
	expires := time.Now().Add(5 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tokens" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["identity"] != "a@b.c" || body["secret"] != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			logins++
			fmt.Fprintf(w, `{"token":"tok","expires":%q}`, time.Now().Add(24*time.Hour).Format(time.RFC3339))
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/nginx/proxy-hosts":
			fmt.Fprint(w, `[{"domain_names":["b.example.com"],"forward_scheme":"http","forward_host":"app","forward_port":80,"enabled":true,"certificate_id":1,"meta":{"nginx_online":true}},
				{"domain_names":["a.example.com"],"forward_host":"x","forward_port":1,"enabled":0,"certificate_id":0,"meta":{"nginx_online":false,"nginx_err":"bad upstream"}}]`)
		case "/api/nginx/certificates":
			fmt.Fprintf(w, `[{"id":1,"nice_name":"Wildcard","domain_names":["*.example.com"],"provider":"letsencrypt","expires_on":%q},
				{"id":2,"nice_name":"Later","domain_names":["z.example.com"],"provider":"other","expires_on":"2099-01-01 00:00:00"}]`, expires)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	w := config.Widget{"url": srv.URL, "email": "a@b.c", "password": "pw"}
	for range 2 {
		got, err := npm(context.Background(), w)
		if err != nil {
			t.Fatal(err)
		}
		d := got.(NPMData)
		if len(d.Hosts) != 2 || d.Hosts[0].Domains[0] != "a.example.com" || d.Disabled != 1 || d.Errors != 1 {
			t.Fatalf("hosts: %+v", d)
		}
		if d.Expiring != 1 || d.Certificates[0].Name != "Wildcard" || d.Certificates[0].Hosts != 1 ||
			d.Certificates[0].Days < 4 || d.Certificates[0].Days > 5 {
			t.Fatalf("certificates: %+v", d.Certificates)
		}
	}
	if logins != 1 {
		t.Fatalf("logged in %d times; the token should be reused", logins)
	}
}

func TestKomodo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Api-Key") != "k" || r.Header.Get("X-Api-Secret") != "s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/read/ListStacks":
			fmt.Fprint(w, `[{"id":"1","name":"media","info":{"state":"running","server_name":"box","services":[{"service":"jellyfin","update_available":true},{"service":"sonarr","update_available":false}]}},
				{"id":"2","name":"apps","info":{"state":"running","services":[{"service":"a"}]}},
				{"id":"3","name":"broken","info":{"state":"unhealthy","services":[]}}]`)
		case "/read/ListServers":
			fmt.Fprint(w, `[{"id":"s1","name":"box","info":{"state":"Ok"}}]`)
		case "/read/ListUpdates":
			fmt.Fprint(w, `{"updates":[{"operation":"UpdateStack","start_ts":1,"success":true,"target":{"id":"1"}},
				{"operation":"DeployStack","start_ts":1790000000000,"success":false,"username":"admin","target":{"type":"Stack","id":"1"}}]}`)
		}
	}))
	defer srv.Close()
	got, err := komodo(context.Background(), config.Widget{"url": srv.URL, "key": "k", "secret": "s"})
	if err != nil {
		t.Fatal(err)
	}
	d := got.(KomodoData)
	if d.Running != 2 || d.Updates != 1 || len(d.Servers) != 1 {
		t.Fatalf("summary: %+v", d)
	}
	names := []string{d.Stacks[0].Name, d.Stacks[1].Name, d.Stacks[2].Name}
	if strings.Join(names, ",") != "broken,media,apps" {
		t.Fatalf("order: %v", names)
	}
	if len(d.Recent) != 1 || d.Recent[0].Operation != "Deploy Stack" || d.Recent[0].Target != "media" || d.Recent[0].Success {
		t.Fatalf("recent: %+v", d.Recent)
	}
}

func TestGatehouseReadsCardAndDiscovery(t *testing.T) {
	expires := time.Now().Add(10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case WellKnownPath:
			fmt.Fprint(w, `{"version":1,"stats":[{"label":"Hosts","value":"2"}],
				"items":[{"title":"convertx","subtitle":"sleeping","action":{"label":"Wake","url":"/api/foyer/wake/convertx"}}]}`)
		case "/api/discovery":
			fmt.Fprintf(w, `{"hosts":[
				{"domains":["books.example.com"],"scheme":"http","forward_host":"shelfloom","forward_port":8000,"enabled":true,"https":true,"certificate":"wild","state":"awake"},
				{"domains":["convert.example.com"],"scheme":"http","forward_host":"convertx","forward_port":3000,"enabled":false,"https":false,"state":"sleeping"}],
				"redirects":[{}],
				"certificates":[{"name":"wild","domains":["*.example.com"],"source":"acme","expires":%q,"hosts":1}],
				"warn_days":14}`, expires)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	got, err := gatehouse(context.Background(), config.Widget{"type": "gatehouse", "url": srv.URL, "key": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	app, ok := AsApp(got)
	if !ok || len(app.Items) != 1 || !app.HasAction("/api/foyer/wake/convertx") {
		t.Fatalf("card: %+v", app)
	}
	p, ok := AsProxy(got)
	if !ok || len(p.Hosts) != 2 || p.Disabled != 1 || p.Redirects != 1 || p.Hosts[1].State != "sleeping" {
		t.Fatalf("proxy: %+v", p)
	}
	if !p.Hosts[0].SSL || p.Hosts[0].ForwardHost != "shelfloom" || p.Expiring != 1 ||
		p.Certificates[0].Provider != "letsencrypt" || p.Certificates[0].Days < 9 {
		t.Fatalf("proxy details: %+v", p)
	}
	if _, err := gatehouse(context.Background(), config.Widget{"type": "gatehouse", "url": srv.URL, "key": "wrong"}); err == nil {
		t.Fatal("a wrong key worked")
	}
	if !IsApp(config.Widget{"type": "gatehouse"}) || IsApp(config.Widget{"type": "npm"}) {
		t.Fatal("IsApp")
	}
}
