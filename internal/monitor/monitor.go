// Package monitor runs the background checks: service pings, Docker
// container state and host stats.
package monitor

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
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
	Name   string `json:"name"`
	State  string `json:"state"` // running | exited | ... | missing
	Status string `json:"status"`
	Health string `json:"health,omitempty"`
}

type ServiceStatus struct {
	Ping      *Ping      `json:"ping,omitempty"`
	Container *Container `json:"container,omitempty"`
	// Check is the service's status from Lookout, when Lookout watches it;
	// it wins over the ping and container state.
	Check *Check `json:"check,omitempty"`
}

// Check is a Lookout check's status.
type Check struct {
	Name      string `json:"name"`
	State     string `json:"state"` // up, down, pending, asleep, paused, maintenance, unknown
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type Monitor struct {
	store     *config.Store
	docker    *docker.Client // nil without a Docker socket
	proc, sys string
	wake      chan struct{}

	// AfterCheck, when set, runs after each round of service checks (alerts
	// use it, so every check counts exactly once).
	AfterCheck func(ctx context.Context)
	// Watched, when set, lists services another monitor (Lookout) checks;
	// they aren't pinged.
	Watched func(ctx context.Context) map[string]bool

	mu         sync.RWMutex
	pings      map[string]Ping // by ping URL, so renames don't lose results
	containers map[string]Container
	cpuHistory []float64
	memHistory []float64
	lastCPU    cpuTimes
}

func New(store *config.Store, dock *docker.Client, proc, sys string) *Monitor {
	return &Monitor{
		store: store, docker: dock, proc: proc, sys: sys,
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
	var watched map[string]bool
	if m.Watched != nil {
		watched = m.Watched(ctx)
	}
	for _, s := range m.store.Config().Services() {
		if s.Ping != "" && !watched[s.ID] {
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
	if m.AfterCheck != nil {
		m.AfterCheck(ctx)
	}
}

func (m *Monitor) fetchContainers(ctx context.Context) map[string]Container {
	if m.docker == nil {
		return nil
	}
	list, err := m.docker.List(ctx)
	if err != nil {
		slog.Debug("docker unavailable", "err", err)
		return nil
	}
	out := make(map[string]Container, len(list))
	for _, c := range list {
		out[c.Name] = Container{Name: c.Name, State: c.State, Status: c.Status, Health: c.Health}
	}
	return out
}

// containerFor is the service's container: the configured one, or else the
// container its status check points at (http://sonarr:8989 → "sonarr").
func (m *Monitor) containerFor(s *config.Service) (Container, bool) {
	if s.Container != "" {
		c, ok := m.containers[s.Container]
		if !ok {
			c = Container{Name: s.Container, State: "missing", Status: "No such container"}
		}
		return c, true
	}
	if s.Ping == "" {
		return Container{}, false
	}
	u, err := url.Parse(config.ExpandEnv(s.Ping))
	if err != nil {
		return Container{}, false
	}
	c, ok := m.containers[u.Hostname()]
	return c, ok
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
		if len(m.containers) > 0 {
			if c, ok := m.containerFor(s); ok {
				st.Container = &c
			}
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
