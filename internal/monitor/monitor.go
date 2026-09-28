// Package monitor runs the background checks: service pings, Docker
// container state and host stats.
package monitor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

const historyLen = 60

type Ping struct {
	State     string `json:"state"` // up | down
	Code      int    `json:"code,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
	CheckedAt int64  `json:"checked_at"`
}

type Container struct {
	State  string `json:"state"` // running | exited | ... | missing
	Status string `json:"status"`
	Health string `json:"health,omitempty"`
}

type ServiceStatus struct {
	Ping      *Ping      `json:"ping,omitempty"`
	Container *Container `json:"container,omitempty"`
}

type Monitor struct {
	store        *config.Store
	dockerSocket string
	proc, sys    string
	wake         chan struct{}

	mu         sync.RWMutex
	pings      map[string]Ping // by ping URL, so renames don't lose results
	containers map[string]Container
	cpuHistory []float64
	memHistory []float64
	lastCPU    cpuTimes
}

func New(store *config.Store, dockerSocket, proc, sys string) *Monitor {
	return &Monitor{
		store: store, dockerSocket: dockerSocket, proc: proc, sys: sys,
		wake:  make(chan struct{}, 1),
		pings: map[string]Ping{}, containers: map[string]Container{},
	}
}

func (m *Monitor) Run(ctx context.Context) {
	go m.every(ctx, func() time.Duration { return 3 * time.Second }, nil, m.sampleSystem)
	m.every(ctx, func() time.Duration {
		return time.Duration(m.store.Config().PingInterval) * time.Second
	}, m.wake, m.checkServices)
}

// Refresh re-checks services right away, e.g. after the config was edited.
func (m *Monitor) Refresh() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Monitor) every(ctx context.Context, interval func() time.Duration, wake chan struct{}, work func(context.Context)) {
	for {
		work(ctx)
		timer := time.NewTimer(interval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

var pingClient = &http.Client{
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed homelab certs
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     90 * time.Second,
	},
	// A redirect (e.g. to a login page) already proves the service is up.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func ping(ctx context.Context, url string) Ping {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Ping{State: "down", Error: "invalid URL", CheckedAt: time.Now().Unix()}
	}
	req.Header.Set("User-Agent", "Foyer")
	resp, err := pingClient.Do(req)
	if err != nil {
		return Ping{State: "down", Error: shortError(err), CheckedAt: time.Now().Unix()}
	}
	resp.Body.Close()
	p := Ping{Code: resp.StatusCode, LatencyMS: time.Since(start).Milliseconds(), CheckedAt: time.Now().Unix()}
	p.State = "up"
	if resp.StatusCode >= 500 {
		p.State = "down"
	}
	return p
}

func shortError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"):
		return "host not found"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "Client.Timeout"), strings.Contains(msg, "deadline exceeded"):
		return "timed out"
	}
	return "unreachable"
}

func (m *Monitor) checkServices(ctx context.Context) {
	urls := map[string]bool{}
	for _, s := range m.store.Config().Services() {
		if s.Ping != "" {
			urls[config.ExpandEnv(s.Ping)] = true
		}
	}
	results := make(map[string]Ping, len(urls))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for url := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := ping(ctx, url)
			mu.Lock()
			results[url] = p
			mu.Unlock()
		}()
	}
	wg.Wait()
	containers := m.fetchContainers(ctx)

	m.mu.Lock()
	m.pings = results
	if containers != nil {
		m.containers = containers
	}
	m.mu.Unlock()
}

func (m *Monitor) fetchContainers(ctx context.Context) map[string]Container {
	if m.dockerSocket == "" {
		return nil
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", m.dockerSocket)
			},
		},
	}
	defer client.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?all=1", nil)
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("docker unavailable", "err", err)
		return nil
	}
	defer resp.Body.Close()
	var items []struct {
		Names  []string
		State  string
		Status string
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil
	}
	out := make(map[string]Container, len(items))
	for _, item := range items {
		c := Container{State: item.State, Status: item.Status}
		for _, h := range []string{"unhealthy", "healthy", "starting"} {
			if strings.Contains(item.Status, "("+h+")") || strings.Contains(item.Status, "health: "+h) {
				c.Health = h
				break
			}
		}
		for _, name := range item.Names {
			out[strings.TrimPrefix(name, "/")] = c
		}
	}
	return out
}

func (m *Monitor) Status() map[string]ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]ServiceStatus{}
	for _, s := range m.store.Config().Services() {
		var st ServiceStatus
		if s.Ping != "" {
			if p, ok := m.pings[config.ExpandEnv(s.Ping)]; ok {
				st.Ping = &p
			}
		}
		if s.Container != "" && len(m.containers) > 0 {
			c, ok := m.containers[s.Container]
			if !ok {
				c = Container{State: "missing", Status: "No such container"}
			}
			st.Container = &c
		}
		out[s.ID] = st
	}
	return out
}

func (m *Monitor) sampleSystem(context.Context) {
	cur, ok := readCPUTimes(m.proc)
	mem, memOK := readMemory(m.proc)
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok {
		if m.lastCPU.total > 0 {
			m.cpuHistory = appendCapped(m.cpuHistory, cpuPercent(m.lastCPU, cur))
		}
		m.lastCPU = cur
	}
	if memOK {
		m.memHistory = appendCapped(m.memHistory, mem.Percent)
	}
}

func appendCapped(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > historyLen {
		s = s[len(s)-historyLen:]
	}
	return s
}

type SystemStats struct {
	CPU struct {
		Percent float64   `json:"percent"`
		History []float64 `json:"history"`
		Cores   int       `json:"cores"`
		Load    []float64 `json:"load"`
	} `json:"cpu"`
	Memory struct {
		Percent float64   `json:"percent"`
		Used    uint64    `json:"used"`
		Total   uint64    `json:"total"`
		History []float64 `json:"history"`
	} `json:"memory"`
	Uptime      float64     `json:"uptime"`
	Temperature *float64    `json:"temperature"`
	Disks       []DiskUsage `json:"disks"`
}

func (m *Monitor) System() SystemStats {
	var s SystemStats
	m.mu.RLock()
	s.CPU.History = append([]float64{}, m.cpuHistory...)
	s.Memory.History = append([]float64{}, m.memHistory...)
	m.mu.RUnlock()
	if n := len(s.CPU.History); n > 0 {
		s.CPU.Percent = s.CPU.History[n-1]
	}
	s.CPU.Cores = countCPUs(m.proc)
	s.CPU.Load = readLoad(m.proc)
	if mem, ok := readMemory(m.proc); ok {
		s.Memory.Percent, s.Memory.Used, s.Memory.Total = mem.Percent, mem.Used, mem.Total
	}
	s.Uptime = readUptime(m.proc)
	s.Temperature = readTemperature(m.sys)
	s.Disks = []DiskUsage{}
	for _, path := range m.store.Config().Header.System.Disks {
		if d, ok := readDisk(path); ok {
			s.Disks = append(s.Disks, d)
		}
	}
	return s
}
