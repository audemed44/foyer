// Package docker is a minimal read-only client for the Docker Engine API over
// its unix socket: container list, resource stats and logs.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("no such container")

type Client struct {
	http *http.Client
	// Streams (logs) have no overall timeout; they end when the caller's
	// context is cancelled.
	stream *http.Client

	mu        sync.Mutex
	prev      map[string]cpuSample // last CPU sample per container id
	stats     map[string]Stats
	statsAt   time.Time
	statsBusy chan struct{}
}

func New(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Client{
		http:      &http.Client{Transport: transport, Timeout: 10 * time.Second},
		stream:    &http.Client{Transport: transport},
		prev:      map[string]cpuSample{},
		statsBusy: make(chan struct{}, 1),
	}
}

func (c *Client) get(ctx context.Context, client *http.Client, path string, query url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("docker: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.get(ctx, c.http, path, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"` // running | exited | restarting | paused | created | dead
	Status  string `json:"status"`
	Health  string `json:"health,omitempty"`
	Created int64  `json:"created"`
	Project string `json:"project,omitempty"` // compose project
	// Ports are the container-side TCP ports, lowest first.
	Ports []int `json:"ports"`
	// Labels holds only the ones Foyer reads: foyer.*, homepage.* and the
	// compose service name.
	Labels map[string]string `json:"labels,omitempty"`
}

func (c *Client) List(ctx context.Context) ([]Container, error) {
	var items []struct {
		ID      string
		Names   []string
		Image   string
		State   string
		Status  string
		Created int64
		Labels  map[string]string
		Ports   []struct {
			PrivatePort int
			Type        string
		}
	}
	if err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &items); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(items))
	for _, it := range items {
		name := it.ID[:min(12, len(it.ID))]
		if len(it.Names) > 0 {
			name = strings.TrimPrefix(it.Names[0], "/")
		}
		ports := []int{}
		for _, p := range it.Ports {
			if p.Type == "tcp" && !slices.Contains(ports, p.PrivatePort) {
				ports = append(ports, p.PrivatePort)
			}
		}
		sort.Ints(ports)
		labels := map[string]string{}
		for k, v := range it.Labels {
			if strings.HasPrefix(k, "foyer.") || strings.HasPrefix(k, "homepage.") ||
				k == "com.docker.compose.service" {
				labels[k] = v
			}
		}
		out = append(out, Container{
			ID: it.ID, Name: name, Image: it.Image, State: it.State, Status: it.Status,
			Health: health(it.Status), Created: it.Created,
			Project: it.Labels["com.docker.compose.project"], Ports: ports, Labels: labels,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func health(status string) string {
	for _, h := range []string{"unhealthy", "healthy", "starting"} {
		if strings.Contains(status, "("+h+")") || strings.Contains(status, "health: "+h) {
			return h
		}
	}
	return ""
}

// Find resolves a container by name or id (prefix) among the current list,
// so user input never reaches the Docker API path unchecked.
func (c *Client) Find(ctx context.Context, ref string) (Container, error) {
	list, err := c.List(ctx)
	if err != nil {
		return Container{}, err
	}
	for _, ct := range list {
		if ct.Name == ref || ct.ID == ref {
			return ct, nil
		}
	}
	if len(ref) >= 6 {
		for _, ct := range list {
			if strings.HasPrefix(ct.ID, ref) {
				return ct, nil
			}
		}
	}
	return Container{}, ErrNotFound
}

// ── Stats ────────────────────────────────────────────────────────────────

type Stats struct {
	CPU      *float64 `json:"cpu"` // percent of one core × cores, like `docker stats`
	MemUsed  uint64   `json:"mem_used"`
	MemLimit uint64   `json:"mem_limit"`
	NetRx    uint64   `json:"net_rx"`
	NetTx    uint64   `json:"net_tx"`
	PIDs     uint64   `json:"pids"`
}

type cpuSample struct{ total, system, cpus uint64 }

type rawStats struct {
	CPUStats    rawCPU `json:"cpu_stats"`
	PreCPUStats rawCPU `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	PIDsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

type rawCPU struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs  uint64 `json:"online_cpus"`
}

func (r rawCPU) sample() cpuSample {
	return cpuSample{r.CPUUsage.TotalUsage, r.SystemUsage, r.OnlineCPUs}
}

// cpuPercent follows the docker CLI: container time over system time, scaled
// by the number of CPUs (so 200% is two full cores).
func cpuPercent(prev, cur cpuSample) *float64 {
	if prev.system == 0 || cur.system <= prev.system || cur.total < prev.total {
		return nil
	}
	cpus := max(cur.cpus, 1)
	v := float64(cur.total-prev.total) / float64(cur.system-prev.system) * float64(cpus) * 100
	v = float64(int64(v*10+0.5)) / 10
	return &v
}

func (r rawStats) toStats(prev cpuSample) Stats {
	s := Stats{MemLimit: r.MemoryStats.Limit, PIDs: r.PIDsStats.Current}
	// Match `docker stats`: page cache that can be reclaimed doesn't count.
	cache := r.MemoryStats.Stats["inactive_file"]
	if cache == 0 {
		cache = r.MemoryStats.Stats["total_inactive_file"]
	}
	if r.MemoryStats.Usage > cache {
		s.MemUsed = r.MemoryStats.Usage - cache
	}
	for _, n := range r.Networks {
		s.NetRx += n.RxBytes
		s.NetTx += n.TxBytes
	}
	if prev.system == 0 {
		prev = r.PreCPUStats.sample()
	}
	s.CPU = cpuPercent(prev, r.CPUStats.sample())
	return s
}

const statsTTL = 4 * time.Second

// Stats returns resource usage for the running containers, keyed by id.
// Samples are taken on demand and reused for a few seconds, so nothing is
// polled while nobody is looking.
func (c *Client) Stats(ctx context.Context, running []Container) map[string]Stats {
	c.mu.Lock()
	fresh := time.Since(c.statsAt) < statsTTL
	cached := c.stats
	c.mu.Unlock()
	if fresh {
		return cached
	}
	// One refresh at a time; concurrent callers get the previous result.
	select {
	case c.statsBusy <- struct{}{}:
		defer func() { <-c.statsBusy }()
	default:
		return cached
	}

	results := make(map[string]Stats, len(running))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ct := range running {
		c.mu.Lock()
		prev, seen := c.prev[ct.ID]
		c.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// one-shot answers at once but without a previous CPU sample;
			// the first time we let Docker take its own (~1s) sample.
			q := url.Values{"stream": {"false"}}
			if seen {
				q.Set("one-shot", "true")
			}
			var raw rawStats
			if err := c.getJSON(ctx, "/containers/"+ct.ID+"/stats", q, &raw); err != nil {
				return
			}
			st := raw.toStats(prev)
			mu.Lock()
			results[ct.ID] = st
			mu.Unlock()
			c.mu.Lock()
			c.prev[ct.ID] = raw.CPUStats.sample()
			c.mu.Unlock()
		}()
	}
	wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.prev {
		if _, ok := results[id]; !ok {
			delete(c.prev, id)
		}
	}
	c.stats, c.statsAt = results, time.Now()
	return results
}

// ── Logs ─────────────────────────────────────────────────────────────────

type LogLine struct {
	Time   string `json:"t"` // RFC 3339 with nanoseconds, as Docker sends it
	Stream string `json:"s"` // out | err
	Text   string `json:"m"`
}

// Logs streams a container's log lines to fn until ctx ends or, when follow
// is false, the log is exhausted. since is a Docker timestamp (RFC 3339 or
// unix seconds) and wins over tail when set.
func (c *Client) Logs(ctx context.Context, ct Container, tail int, since string, follow bool, fn func(LogLine) error) error {
	var info struct {
		Config struct{ Tty bool }
	}
	if err := c.getJSON(ctx, "/containers/"+ct.ID+"/json", nil, &info); err != nil {
		return err
	}
	q := url.Values{
		"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"},
		"follow": {strconv.FormatBool(follow)},
	}
	if since != "" {
		q.Set("since", since)
	} else {
		q.Set("tail", strconv.Itoa(max(tail, 0)))
	}
	resp, err := c.get(ctx, c.stream, "/containers/"+ct.ID+"/logs", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if info.Config.Tty {
		return splitLines(resp.Body, "out", fn)
	}
	return demux(resp.Body, fn)
}

// demux reads Docker's multiplexed log stream: each frame is an 8-byte
// header (stream type, 3 zero bytes, big-endian length) and a payload.
func demux(r io.Reader, fn func(LogLine) error) error {
	header := make([]byte, 8)
	partial := map[string]string{}
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			return ignoreEOF(err)
		}
		size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return ignoreEOF(err)
		}
		stream := "out"
		if header[0] == 2 {
			stream = "err"
		}
		text := partial[stream] + string(payload)
		lines := strings.Split(text, "\n")
		partial[stream] = lines[len(lines)-1]
		for _, line := range lines[:len(lines)-1] {
			if err := fn(parseLine(line, stream)); err != nil {
				return err
			}
		}
	}
}

func splitLines(r io.Reader, stream string, fn func(LogLine) error) error {
	buf := make([]byte, 32*1024)
	partial := ""
	for {
		n, err := r.Read(buf)
		if n > 0 {
			lines := strings.Split(partial+string(buf[:n]), "\n")
			partial = lines[len(lines)-1]
			for _, line := range lines[:len(lines)-1] {
				if ferr := fn(parseLine(line, stream)); ferr != nil {
					return ferr
				}
			}
		}
		if err != nil {
			return ignoreEOF(err)
		}
	}
}

func parseLine(line, stream string) LogLine {
	line = strings.TrimSuffix(line, "\r")
	ts, text, ok := strings.Cut(line, " ")
	if !ok || len(ts) < 20 || ts[4] != '-' {
		return LogLine{Stream: stream, Text: line}
	}
	return LogLine{Time: ts, Stream: stream, Text: text}
}

func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
