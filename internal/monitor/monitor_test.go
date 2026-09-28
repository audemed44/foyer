package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/audemed44/foyer/internal/config"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcReaders(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 1 1 1\ncpu1 1 1 1 1\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 250 kB\n")
	write(t, filepath.Join(proc, "uptime"), "3600.5 100.0\n")
	write(t, filepath.Join(proc, "loadavg"), "0.50 0.25 0.10 1/100 42\n")

	prev, _ := readCPUTimes(proc)
	write(t, filepath.Join(proc, "stat"), "cpu  200 0 200 750 150 0 0 0 0 0\n")
	cur, _ := readCPUTimes(proc)
	if got := cpuPercent(prev, cur); got != 66.7 {
		t.Fatalf("cpu = %v", got)
	}
	mem, ok := readMemory(proc)
	if !ok || mem.Percent != 75 || mem.Total != 1000*1024 {
		t.Fatalf("mem = %+v", mem)
	}
	if readUptime(proc) != 3600.5 {
		t.Fatal("uptime")
	}
	if l := readLoad(proc); len(l) != 3 || l[0] != 0.5 {
		t.Fatalf("load = %v", l)
	}
}

func TestReadTemperaturePrefersCPUSensors(t *testing.T) {
	sys := t.TempDir()
	write(t, filepath.Join(sys, "class/hwmon/hwmon0/name"), "nvme\n")
	write(t, filepath.Join(sys, "class/hwmon/hwmon0/temp1_input"), "70000\n")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/name"), "coretemp\n")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/temp1_input"), "48000\n")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/temp2_input"), "52500\n")
	if got := readTemperature(sys); got == nil || *got != 52.5 {
		t.Fatalf("temp = %v", got)
	}
	if readTemperature(t.TempDir()) != nil {
		t.Fatal("no sensors should be nil")
	}
}

func TestCheckServices(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer up.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer broken.Close()

	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "g", Services: []config.Service{
		{Name: "Up", Ping: up.URL},
		{Name: "Broken", Ping: broken.URL},
		{Name: "Gone", Ping: "http://127.0.0.1:1"},
		{Name: "Plain"},
	}}}
	store.WriteInitial(cfg)

	m := New(store, "", "/proc", "/sys")
	m.checkServices(context.Background())
	st := m.Status()
	if st["up"].Ping.State != "up" || st["up"].Ping.Code != 302 {
		t.Fatalf("up: %+v", st["up"].Ping)
	}
	if st["broken"].Ping.State != "down" {
		t.Fatal("5xx should be down")
	}
	if st["gone"].Ping.State != "down" || st["gone"].Ping.Error != "connection refused" {
		t.Fatalf("gone: %+v", st["gone"].Ping)
	}
	if st["plain"].Ping != nil {
		t.Fatal("services without a ping have no ping status")
	}
}
