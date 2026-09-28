package monitor

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Host stats are read straight from /proc and /sys (Linux only). Inside a
// container these still describe the host: CPU, memory, load and uptime
// aren't namespaced, and sysfs exposes the hardware sensors.

type cpuTimes struct{ idle, total uint64 }

func readCPUTimes(proc string) (cpuTimes, bool) {
	f, err := os.Open(filepath.Join(proc, "stat"))
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return cpuTimes{}, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}
	var t cpuTimes
	for i, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		t.total += v
		if i == 3 || i == 4 { // idle, iowait
			t.idle += v
		}
	}
	return t, true
}

func cpuPercent(prev, cur cpuTimes) float64 {
	total := float64(cur.total - prev.total)
	if total <= 0 {
		return 0
	}
	return round1(100 * (1 - float64(cur.idle-prev.idle)/total))
}

type memInfo struct {
	Total, Used uint64
	Percent     float64
}

func readMemory(proc string) (memInfo, bool) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return memInfo{}, false
	}
	defer f.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = v * 1024
		}
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	if total == 0 {
		return memInfo{}, false
	}
	used := total - available
	return memInfo{total, used, round1(100 * float64(used) / float64(total))}, true
}

func readUptime(proc string) float64 {
	data, err := os.ReadFile(filepath.Join(proc, "uptime"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func readLoad(proc string) []float64 {
	data, err := os.ReadFile(filepath.Join(proc, "loadavg"))
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	out := make([]float64, 0, 3)
	for _, f := range fields[:min(3, len(fields))] {
		v, _ := strconv.ParseFloat(f, 64)
		out = append(out, v)
	}
	return out
}

func countCPUs(proc string) int {
	f, err := os.Open(filepath.Join(proc, "stat"))
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return n
}

// readTemperature returns the hottest CPU sensor in °C, preferring known CPU
// drivers over other hwmon devices (NVMe, wifi, ...).
func readTemperature(sys string) *float64 {
	dirs, _ := filepath.Glob(filepath.Join(sys, "class", "hwmon", "hwmon*"))
	preferred := map[string]bool{
		"coretemp": true, "k10temp": true, "zenpower": true, "cpu_thermal": true,
	}
	var best, fallback float64
	for _, dir := range dirs {
		name, _ := os.ReadFile(filepath.Join(dir, "name"))
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, input := range inputs {
			raw, err := os.ReadFile(input)
			if err != nil {
				continue
			}
			milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
			if err != nil || milli <= 0 {
				continue
			}
			c := milli / 1000
			if preferred[strings.TrimSpace(string(name))] {
				best = max(best, c)
			} else if strings.TrimSpace(string(name)) == "acpitz" {
				fallback = max(fallback, c)
			}
		}
	}
	if best == 0 {
		best = fallback
	}
	if best == 0 {
		return nil
	}
	v := round1(best)
	return &v
}

type DiskUsage struct {
	Path    string  `json:"path"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

func readDisk(path string) (DiskUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return DiskUsage{}, false
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - st.Bfree*uint64(st.Bsize)
	// Match `df`: percent of the space available to users.
	pct := round1(100 * float64(used) / float64(used+free))
	return DiskUsage{path, used, total, pct}, true
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
