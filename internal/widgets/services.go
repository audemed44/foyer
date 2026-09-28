package widgets

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/audemed44/foyer/internal/config"
)

type kumaMonitor struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"` // up | down | pending | maintenance | unknown
	Uptime  *float64 `json:"uptime"` // 24h, 0-100
	Ping    *int     `json:"ping"`
	History []string `json:"history"` // recent beats, oldest first
}

// uptimeKuma reads a public Uptime Kuma status page.
// Settings: url, slug.
func uptimeKuma(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "slug"); err != nil {
		return nil, err
	}
	base, slug := w.String("url"), w.String("slug")
	var page struct {
		Config struct {
			Title string `json:"title"`
		} `json:"config"`
		Incident *struct {
			Title string `json:"title"`
		} `json:"incident"`
		PublicGroupList []struct {
			MonitorList []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"monitorList"`
		} `json:"publicGroupList"`
	}
	if err := getJSON(ctx, join(base, "/api/status-page/"+slug), &page); err != nil {
		return nil, err
	}
	var beats struct {
		HeartbeatList map[string][]struct {
			Status int  `json:"status"`
			Ping   *int `json:"ping"`
		} `json:"heartbeatList"`
		UptimeList map[string]float64 `json:"uptimeList"`
	}
	if err := getJSON(ctx, join(base, "/api/status-page/heartbeat/"+slug), &beats); err != nil {
		return nil, err
	}
	statusName := map[int]string{0: "down", 1: "up", 2: "pending", 3: "maintenance"}
	monitors := []kumaMonitor{}
	up, down := 0, 0
	var uptimeSum float64
	uptimeN := 0
	for _, g := range page.PublicGroupList {
		for _, m := range g.MonitorList {
			id := strconv.Itoa(m.ID)
			km := kumaMonitor{Name: m.Name, Status: "unknown", History: []string{}}
			list := beats.HeartbeatList[id]
			for _, b := range list[max(0, len(list)-30):] {
				km.History = append(km.History, statusName[b.Status])
			}
			if n := len(list); n > 0 {
				km.Status = statusName[list[n-1].Status]
				km.Ping = list[n-1].Ping
			}
			if u, ok := beats.UptimeList[id+"_24"]; ok {
				pct := u * 100
				km.Uptime = &pct
				uptimeSum += pct
				uptimeN++
			}
			switch km.Status {
			case "up":
				up++
			case "down":
				down++
			}
			monitors = append(monitors, km)
		}
	}
	// Problems first, then alphabetical.
	sort.SliceStable(monitors, func(i, j int) bool {
		if (monitors[i].Status == "down") != (monitors[j].Status == "down") {
			return monitors[i].Status == "down"
		}
		return strings.ToLower(monitors[i].Name) < strings.ToLower(monitors[j].Name)
	})
	out := map[string]any{
		"title": page.Config.Title, "up": up, "down": down, "total": len(monitors),
		"monitors": monitors,
	}
	if uptimeN > 0 {
		out["uptime"] = uptimeSum / float64(uptimeN)
	}
	if page.Incident != nil && page.Incident.Title != "" {
		out["incident"] = page.Incident.Title
	}
	return out, nil
}

// speedtest reads the latest result from Speedtest Tracker.
// Settings: url, key (API token, needed for version 2), version (1 or 2, default 2).
func speedtest(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url"); err != nil {
		return nil, err
	}
	if w.Int("version", 2) == 1 {
		var v1 struct {
			Data struct {
				Download  float64 `json:"download"` // Mbps
				Upload    float64 `json:"upload"`
				Ping      float64 `json:"ping"`
				CreatedAt string  `json:"created_at"`
			} `json:"data"`
		}
		if err := getJSON(ctx, join(w.String("url"), "/api/speedtest/latest"), &v1); err != nil {
			return nil, err
		}
		d := v1.Data
		return map[string]any{
			"download_mbps": d.Download, "upload_mbps": d.Upload, "ping_ms": d.Ping,
			"at": d.CreatedAt,
		}, nil
	}
	if err := required(w, "key"); err != nil {
		return nil, err
	}
	var v2 struct {
		Data struct {
			Ping         float64 `json:"ping"`
			DownloadBits float64 `json:"download_bits"`
			UploadBits   float64 `json:"upload_bits"`
			Download     float64 `json:"download"` // bytes per second
			Upload       float64 `json:"upload"`
			CreatedAt    string  `json:"created_at"`
		} `json:"data"`
	}
	err := getJSON(ctx, join(w.String("url"), "/api/v1/results/latest"), &v2,
		"Authorization", "Bearer "+w.String("key"))
	if err != nil {
		return nil, err
	}
	d := v2.Data
	down, up := d.DownloadBits, d.UploadBits
	if down == 0 {
		down = d.Download * 8
	}
	if up == 0 {
		up = d.Upload * 8
	}
	if down == 0 && up == 0 && d.Ping == 0 {
		return nil, fmt.Errorf("no speedtest results yet")
	}
	return map[string]any{
		"download_mbps": down / 1e6, "upload_mbps": up / 1e6, "ping_ms": d.Ping,
		"at": d.CreatedAt,
	}, nil
}
