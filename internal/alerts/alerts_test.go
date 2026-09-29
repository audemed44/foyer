package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type recorder struct{ sent []Event }

func engine(t *testing.T, path string) (*Engine, *recorder) {
	e := New(path)
	r := &recorder{}
	e.send = func(_ context.Context, _ config.Alerts, ev Event) error {
		r.sent = append(r.sent, ev)
		return nil
	}
	return e, r
}

var cfg = config.Alerts{AppriseURL: "http://apprise/notify/x"}

func down(after int) Problem {
	return Problem{Key: "service:web", Title: "Web is down", Level: "failure", After: after, Recovered: "Web is back up"}
}

func TestAlertsOnceAfterNChecksAndRecovers(t *testing.T) {
	e, r := engine(t, filepath.Join(t.TempDir(), "alerts.json"))
	ctx := context.Background()
	scope := []string{"service:"}
	e.Evaluate(ctx, cfg, scope, []Problem{down(2)})
	if len(r.sent) != 0 {
		t.Fatal("alerted on the first failed check")
	}
	e.Evaluate(ctx, cfg, scope, []Problem{down(2)})
	e.Evaluate(ctx, cfg, scope, []Problem{down(2)})
	if len(r.sent) != 1 || r.sent[0].Title != "Web is down" || r.sent[0].Level != "failure" {
		t.Fatalf("sent: %+v", r.sent)
	}
	e.Evaluate(ctx, cfg, scope, nil)
	if len(r.sent) != 2 || r.sent[1].Title != "Web is back up" || r.sent[1].Level != "success" {
		t.Fatalf("recovery: %+v", r.sent)
	}
	open, history := e.Snapshot()
	if len(open) != 0 || len(history) != 2 || history[0].Title != "Web is back up" {
		t.Fatalf("snapshot: %+v %+v", open, history)
	}
}

func TestFlapResetsTheCount(t *testing.T) {
	e, r := engine(t, filepath.Join(t.TempDir(), "alerts.json"))
	ctx := context.Background()
	for range 3 {
		e.Evaluate(ctx, cfg, []string{"service:"}, []Problem{down(2)})
		e.Evaluate(ctx, cfg, []string{"service:"}, nil)
	}
	if len(r.sent) != 0 {
		t.Fatalf("a flapping check alerted: %+v", r.sent)
	}
}

func TestScopeKeepsOtherChecksOpen(t *testing.T) {
	e, r := engine(t, filepath.Join(t.TempDir(), "alerts.json"))
	ctx := context.Background()
	backup := Problem{Key: "backup:/data", Title: "Backup overdue", Level: "failure", After: 1}
	e.Evaluate(ctx, cfg, []string{"backup:"}, []Problem{backup})
	// The service check runs often and doesn't look at backups.
	e.Evaluate(ctx, cfg, []string{"service:"}, nil)
	if len(r.sent) != 1 {
		t.Fatalf("the backup alert was resolved by an unrelated check: %+v", r.sent)
	}
	e.Forget("backup:")
	if open, _ := e.Snapshot(); len(open) != 0 {
		t.Fatalf("forget: %+v", open)
	}
	if len(r.sent) != 1 {
		t.Fatal("forget notified")
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	e, _ := engine(t, path)
	e.Evaluate(context.Background(), cfg, []string{"service:"}, []Problem{down(1)})

	again, r := engine(t, path)
	again.Evaluate(context.Background(), cfg, []string{"service:"}, []Problem{down(1)})
	if len(r.sent) != 0 {
		t.Fatal("re-alerted an open problem after a restart")
	}
	again.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	again.Evaluate(context.Background(), cfg, []string{"service:"}, nil)
	if len(r.sent) != 1 || r.sent[0].Title != "Web is back up" || r.sent[0].Body != "After 10 min." {
		t.Fatalf("recovery after restart: %+v", r.sent)
	}
}

func TestSendToApprise(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notify/empty":
			w.WriteHeader(http.StatusNoContent)
			return
		case "/notify/foyer":
		default:
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	err := Send(context.Background(), config.Alerts{AppriseURL: srv.URL + "/notify/foyer", Tag: "homelab"},
		Event{Level: "failure", Title: "Web is down", Body: "connection refused"})
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Web is down" || got["type"] != "failure" || got["tag"] != "homelab" || got["body"] != "connection refused" {
		t.Fatalf("apprise got %v", got)
	}
	for _, path := range []string{"/wrong", "/notify/empty"} {
		if err := Send(context.Background(), config.Alerts{AppriseURL: srv.URL + path}, Event{Title: "x"}); err == nil {
			t.Fatalf("%s should be an error", path)
		}
	}
}
