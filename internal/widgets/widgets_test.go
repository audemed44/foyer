package widgets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

func TestUptimeKuma(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status-page/main":
			w.Write([]byte(`{"config":{"title":"Main"},"publicGroupList":[{"monitorList":[{"id":1,"name":"Web"},{"id":2,"name":"DB"}]}]}`))
		case "/api/status-page/heartbeat/main":
			w.Write([]byte(`{"heartbeatList":{"1":[{"status":1,"ping":20}],"2":[{"status":1},{"status":0}]},"uptimeList":{"1_24":1,"2_24":0.5}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	got, err := uptimeKuma(context.Background(), config.Widget{"url": srv.URL + "/", "slug": "main"})
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["up"] != 1 || m["down"] != 1 || m["uptime"] != 75.0 {
		t.Fatalf("summary: %v", m)
	}
	monitors := m["monitors"].([]kumaMonitor)
	if monitors[0].Name != "DB" || monitors[0].Status != "down" || len(monitors[0].History) != 2 {
		t.Fatalf("down monitors should sort first: %+v", monitors)
	}
}

func TestSpeedtestV2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":{"ping":4.2,"download_bits":940000000,"upload_bits":0,"upload":60000000,"created_at":"2026-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	got, err := speedtest(context.Background(), config.Widget{"url": srv.URL, "key": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["download_mbps"] != 940.0 || m["upload_mbps"] != 480.0 {
		t.Fatalf("got %v", m)
	}
	_, err = speedtest(context.Background(), config.Widget{"url": srv.URL, "key": "wrong"})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected a credentials error, got %v", err)
	}
	if _, err := speedtest(context.Background(), config.Widget{"url": srv.URL}); err == nil {
		t.Fatal("v2 without a key should fail")
	}
}

func TestFetchCachesAndInvalidatesOnEdit(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Write([]byte(`{"data":{"download":1,"upload":1,"ping":1}}`))
	}))
	defer srv.Close()
	t.Setenv("SPEED_URL", srv.URL)
	s := NewService()
	w := config.Widget{"type": "speedtest", "url": "${SPEED_URL}", "version": 1}
	for range 3 {
		if _, err := s.Fetch(context.Background(), "speed", w); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Fatalf("expected one upstream call, got %d", hits)
	}
	w2 := config.Widget{"type": "speedtest", "url": "${SPEED_URL}", "version": 1, "span": 2}
	s.Fetch(context.Background(), "speed", w2)
	if hits != 2 {
		t.Fatal("editing the widget should bypass the cache")
	}
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	s.Fetch(context.Background(), "speed", w2)
	if hits != 3 {
		t.Fatal("stale entries should refetch")
	}
	if _, err := s.Fetch(context.Background(), "x", config.Widget{"type": "nope"}); err == nil {
		t.Fatal("unknown type should fail")
	}
}

const ics = "BEGIN:VCALENDAR\r\n" +
	"BEGIN:VEVENT\r\nSUMMARY:Show S01E0\r\n 2\r\nDTSTART;VALUE=DATE:20260305\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nSUMMARY:Standup\r\nDTSTART:20260302T090000Z\r\nDTEND:20260302T091500Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nSUMMARY:Past\r\nDTSTART:20250101T090000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nSUMMARY:Weekly\\, fun\r\nDTSTART;TZID=Asia/Kolkata:20260226T200000\r\nRRULE:FREQ=WEEKLY;UNTIL=20260320T000000Z\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseICal(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 14)
	events := ParseICal(strings.NewReader(ics), from, to)
	var titles []string
	for _, e := range events {
		titles = append(titles, e.Title)
	}
	want := "Standup,Standup,Standup,Show S01E02,Weekly, fun,Weekly, fun"
	if got := strings.Join(titles, ","); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !events[3].AllDay || events[3].Start != "2026-03-05" {
		t.Fatalf("all-day event: %+v", events[3])
	}
}

func TestCalendarWidget(t *testing.T) {
	now := time.Now().UTC()
	body := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Soon\nDTSTART:" + now.Add(48*time.Hour).Format("20060102T150405Z") +
		"\nEND:VEVENT\nEND:VCALENDAR\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	got, err := calendar(context.Background(), config.Widget{"url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if events := got.(map[string]any)["events"].([]Event); len(events) != 1 || events[0].Title != "Soon" {
		t.Fatalf("got %v", got)
	}
}
