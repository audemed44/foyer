package server

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/monitor"
)

func fakeDocker(t *testing.T, logQuery *string) *docker.Client {
	t.Helper()
	dir, _ := os.MkdirTemp("", "dk")
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"Id":"abcdef123456","Names":["/web"],"Image":"nginx","State":"running","Status":"Up"}]`)
	})
	mux.HandleFunc("GET /containers/{id}/stats", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"memory_stats":{"usage":2048,"limit":4096}}`)
	})
	mux.HandleFunc("GET /containers/{id}/json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"Config":{"Tty":false}}`)
	})
	mux.HandleFunc("GET /containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		*logQuery = r.URL.RawQuery
		text := "2026-01-01T00:00:00.5Z hello world\n"
		h := make([]byte, 8)
		h[0] = 1
		binary.BigEndian.PutUint32(h[4:], uint32(len(text)))
		w.Write(append(h, text...))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return docker.New(sock)
}

func containerServer(t *testing.T, dock *docker.Client) http.Handler {
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	return New(store, monitor.New(store, dock, "/proc", "/sys"), dock, dir, nil).Handler()
}

func TestContainersList(t *testing.T) {
	var q string
	h := containerServer(t, fakeDocker(t, &q))
	rec := do(h, "GET", "/api/containers", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"web"`) || !strings.Contains(rec.Body.String(), `"mem_used":2048`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestContainerLogsStream(t *testing.T) {
	var q string
	h := containerServer(t, fakeDocker(t, &q))

	rec := do(h, "GET", "/api/containers/web/logs?tail=50", "")
	body := rec.Body.String()
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"id: 2026-01-01T00:00:00.5Z", `"m":"hello world"`, `"s":"out"`, "event: end"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if !strings.Contains(q, "tail=50") || !strings.Contains(q, "follow=true") {
		t.Fatalf("query %s", q)
	}

	// A reconnect resumes from the last event instead of replaying the tail.
	req := httptest.NewRequest("GET", "/api/containers/web/logs", nil)
	req.Header.Set("Last-Event-ID", "2026-01-01T00:00:00.5Z")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(q, "since=2026-01-01T00") || strings.Contains(q, "tail=") {
		t.Fatalf("resume query %s", q)
	}

	if rec := do(h, "GET", "/api/containers/nope/logs", ""); rec.Code != 404 {
		t.Fatalf("unknown container: %d", rec.Code)
	}
}

func TestDiscover(t *testing.T) {
	var q string
	h := containerServer(t, fakeDocker(t, &q))
	rec := do(h, "GET", "/api/discover", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"container":"web"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestContainersWithoutDocker(t *testing.T) {
	h, _ := setup(t)
	if rec := do(h, "GET", "/api/containers", ""); rec.Code != 503 {
		t.Fatalf("got %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/discover", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("discover without docker: %d %s", rec.Code, rec.Body)
	}
}
