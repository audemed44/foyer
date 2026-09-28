package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// frame builds one multiplexed log frame (stream 1 = stdout, 2 = stderr).
func frame(stream byte, text string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(text)))
	return append(h, text...)
}

type fakeDocker struct {
	statsCalls atomic.Int32
	oneShot    atomic.Int32
	logQuery   atomic.Value
}

// serve starts a fake Docker API on a unix socket and returns a client for it.
func (f *fakeDocker) serve(t *testing.T) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[
			{"Id":"aaaaaaaaaaaa1111","Names":["/web"],"Image":"nginx","State":"running","Status":"Up 2 hours (healthy)","Labels":{"com.docker.compose.project":"main"}},
			{"Id":"bbbbbbbbbbbb2222","Names":["/db"],"Image":"postgres","State":"exited","Status":"Exited (0) 1 day ago","Labels":{}},
			{"Id":"cccccccccccc3333","Names":["/tty"],"Image":"alpine","State":"running","Status":"Up 1 minute","Labels":{}}
		]`)
	})
	mux.HandleFunc("GET /containers/{id}/stats", func(w http.ResponseWriter, r *http.Request) {
		n := f.statsCalls.Add(1)
		if r.URL.Query().Get("one-shot") == "true" {
			f.oneShot.Add(1)
		}
		// Each call advances the counters: +50 container ticks per +100 system ticks.
		total, system := 1000+50*uint64(n), 10000+100*uint64(n)
		fmt.Fprintf(w, `{
			"cpu_stats":{"cpu_usage":{"total_usage":%d},"system_cpu_usage":%d,"online_cpus":2},
			"precpu_stats":{"cpu_usage":{"total_usage":%d},"system_cpu_usage":%d,"online_cpus":2},
			"memory_stats":{"usage":1000,"limit":8000,"stats":{"inactive_file":200}},
			"networks":{"eth0":{"rx_bytes":10,"tx_bytes":20},"eth1":{"rx_bytes":1,"tx_bytes":2}},
			"pids_stats":{"current":7}
		}`, total, system, total-25, system-100)
	})
	mux.HandleFunc("GET /containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"Config":{"Tty":%t}}`, strings.HasPrefix(r.PathValue("id"), "cccc"))
	})
	mux.HandleFunc("GET /containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		f.logQuery.Store(r.URL.RawQuery)
		if strings.HasPrefix(r.PathValue("id"), "cccc") {
			fmt.Fprint(w, "2026-01-01T00:00:00.000000001Z tty line one\r\n2026-01-01T00:00:01.000000001Z tty line two\n")
			return
		}
		var buf bytes.Buffer
		buf.Write(frame(1, "2026-01-01T00:00:00.000000001Z hello\n2026-01-01T00:00:01.0000"))
		buf.Write(frame(1, "00001Z split across frames\n"))
		buf.Write(frame(2, "2026-01-01T00:00:02.000000001Z \x1b[31moops\x1b[0m\n"))
		w.Write(buf.Bytes())
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return New(sock)
}

func TestListAndFind(t *testing.T) {
	c := (&fakeDocker{}).serve(t)
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != "db" || list[2].Name != "web" {
		t.Fatalf("list should be sorted by name: %+v", list)
	}
	if list[2].Health != "healthy" || list[2].Project != "main" {
		t.Fatalf("web: %+v", list[2])
	}
	for _, ref := range []string{"web", "aaaaaaaaaaaa1111", "aaaaaa"} {
		if ct, err := c.Find(context.Background(), ref); err != nil || ct.Name != "web" {
			t.Fatalf("Find(%q) = %+v, %v", ref, ct, err)
		}
	}
	for _, ref := range []string{"nope", "aaa", "../../etc"} {
		if _, err := c.Find(context.Background(), ref); err != ErrNotFound {
			t.Fatalf("Find(%q) should be ErrNotFound, got %v", ref, err)
		}
	}
}

func TestStats(t *testing.T) {
	f := &fakeDocker{}
	c := f.serve(t)
	running := []Container{{ID: "aaaaaaaaaaaa1111", Name: "web"}}

	first := c.Stats(context.Background(), running)["aaaaaaaaaaaa1111"]
	// First sample uses Docker's precpu: 25 / 100 × 2 CPUs = 50%.
	if first.CPU == nil || *first.CPU != 50 {
		t.Fatalf("first cpu = %v", first.CPU)
	}
	if first.MemUsed != 800 || first.MemLimit != 8000 || first.NetRx != 11 || first.NetTx != 22 || first.PIDs != 7 {
		t.Fatalf("stats = %+v", first)
	}
	if f.oneShot.Load() != 0 {
		t.Fatal("the first sample needs Docker's own precpu, not one-shot")
	}

	// Cached within the TTL.
	c.Stats(context.Background(), running)
	if f.statsCalls.Load() != 1 {
		t.Fatalf("expected a cached result, got %d calls", f.statsCalls.Load())
	}

	// After the TTL: one-shot, with CPU measured against our previous sample
	// (+50 / +100 × 2 = 100%).
	c.mu.Lock()
	c.statsAt = c.statsAt.Add(-statsTTL)
	c.mu.Unlock()
	second := c.Stats(context.Background(), running)["aaaaaaaaaaaa1111"]
	if f.oneShot.Load() != 1 || second.CPU == nil || *second.CPU != 100 {
		t.Fatalf("second cpu = %v (one-shot calls %d)", second.CPU, f.oneShot.Load())
	}
}

func TestCPUPercentGuards(t *testing.T) {
	if cpuPercent(cpuSample{}, cpuSample{1, 1, 1}) != nil {
		t.Fatal("no previous sample")
	}
	if cpuPercent(cpuSample{10, 10, 1}, cpuSample{5, 20, 1}) != nil {
		t.Fatal("counter reset (container restarted)")
	}
}

func collect(t *testing.T, c *Client, name, since string) []LogLine {
	t.Helper()
	ct, err := c.Find(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	var lines []LogLine
	err = c.Logs(context.Background(), ct, 100, since, false, func(l LogLine) error {
		lines = append(lines, l)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestLogsDemux(t *testing.T) {
	f := &fakeDocker{}
	c := f.serve(t)
	lines := collect(t, c, "web", "")
	if len(lines) != 3 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Text != "hello" || lines[0].Time != "2026-01-01T00:00:00.000000001Z" || lines[0].Stream != "out" {
		t.Fatalf("line 0 = %+v", lines[0])
	}
	if lines[1].Text != "split across frames" {
		t.Fatalf("a line split over two frames should be rejoined: %+v", lines[1])
	}
	if lines[2].Stream != "err" || !strings.Contains(lines[2].Text, "oops") {
		t.Fatalf("stderr line = %+v", lines[2])
	}
	if q := f.logQuery.Load().(string); !strings.Contains(q, "tail=100") || !strings.Contains(q, "timestamps=1") {
		t.Fatalf("query = %s", q)
	}

	collect(t, c, "web", "2026-01-01T00:00:01Z")
	if q := f.logQuery.Load().(string); !strings.Contains(q, "since=") || strings.Contains(q, "tail=") {
		t.Fatalf("since should replace tail: %s", q)
	}
}

func TestLogsTTY(t *testing.T) {
	c := (&fakeDocker{}).serve(t)
	lines := collect(t, c, "tty", "")
	if len(lines) != 2 || lines[0].Text != "tty line one" || lines[1].Text != "tty line two" {
		t.Fatalf("tty lines = %+v", lines)
	}
}

func TestParseLineWithoutTimestamp(t *testing.T) {
	if l := parseLine("plain text", "out"); l.Time != "" || l.Text != "plain text" {
		t.Fatalf("got %+v", l)
	}
}
