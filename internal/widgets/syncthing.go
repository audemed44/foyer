package widgets

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type SyncFolder struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
	// State is Syncthing's: idle, scanning, syncing, sync-preparing, error, …
	// or "paused".
	State       string  `json:"state"`
	Error       string  `json:"error,omitempty"`
	PullErrors  int     `json:"pull_errors,omitempty"`
	GlobalBytes int64   `json:"global_bytes"`
	NeedBytes   int64   `json:"need_bytes"`
	NeedItems   int     `json:"need_items"`
	Completion  float64 `json:"completion"` // 0-100, of the local copy
	Devices     int     `json:"devices"`    // shared with, besides this one
}

type SyncDevice struct {
	Name      string     `json:"name"`
	Connected bool       `json:"connected"`
	Paused    bool       `json:"paused,omitempty"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
}

type SyncthingData struct {
	Folders   []SyncFolder `json:"folders"`
	Devices   []SyncDevice `json:"devices"`
	InSync    int          `json:"in_sync"`
	Connected int          `json:"connected"`
	Problems  int          `json:"problems"`
}

// syncthing reads folder and device state from Syncthing's REST API.
// Settings: url, key (the GUI's API key).
func syncthing(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "key"); err != nil {
		return nil, err
	}
	base := w.String("url")
	auth := []string{"X-API-Key", w.String("key")}
	get := func(path string, out any) error { return getJSON(ctx, join(base, path), out, auth...) }

	var status struct {
		MyID string `json:"myID"`
	}
	var folders []struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		Path    string `json:"path"`
		Paused  bool   `json:"paused"`
		Devices []struct {
			DeviceID string `json:"deviceID"`
		} `json:"devices"`
	}
	var devices []struct {
		DeviceID string `json:"deviceID"`
		Name     string `json:"name"`
		Paused   bool   `json:"paused"`
	}
	var conns struct {
		Connections map[string]struct {
			Connected bool `json:"connected"`
			Paused    bool `json:"paused"`
		} `json:"connections"`
	}
	var stats map[string]struct {
		LastSeen time.Time `json:"lastSeen"`
	}
	for _, step := range []struct {
		path string
		out  any
	}{
		{"/rest/system/status", &status},
		{"/rest/config/folders", &folders},
		{"/rest/config/devices", &devices},
		{"/rest/system/connections", &conns},
		{"/rest/stats/device", &stats},
	} {
		if err := get(step.path, step.out); err != nil {
			return nil, err
		}
	}

	out := SyncthingData{Folders: make([]SyncFolder, len(folders)), Devices: []SyncDevice{}}
	var wg sync.WaitGroup
	for i, f := range folders {
		label := f.Label
		if label == "" {
			label = f.ID
		}
		out.Folders[i] = SyncFolder{ID: f.ID, Label: label, Path: f.Path, State: "paused",
			Completion: 100, Devices: max(0, len(f.Devices)-1)}
		if f.Paused {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var db struct {
				State       string `json:"state"`
				Error       string `json:"error"`
				PullErrors  int    `json:"pullErrors"`
				GlobalBytes int64  `json:"globalBytes"`
				NeedBytes   int64  `json:"needBytes"`
				NeedItems   int    `json:"needTotalItems"`
			}
			sf := &out.Folders[i]
			if err := get("/rest/db/status?folder="+url.QueryEscape(f.ID), &db); err != nil {
				sf.State, sf.Error = "error", err.Error()
				return
			}
			sf.State, sf.Error, sf.PullErrors = db.State, db.Error, db.PullErrors
			sf.GlobalBytes, sf.NeedBytes, sf.NeedItems = db.GlobalBytes, db.NeedBytes, db.NeedItems
			if db.GlobalBytes > 0 {
				sf.Completion = float64(db.GlobalBytes-db.NeedBytes) / float64(db.GlobalBytes) * 100
			}
		}()
	}
	wg.Wait()
	for _, f := range out.Folders {
		switch {
		case f.State == "error" || f.PullErrors > 0:
			out.Problems++
		case f.State == "idle" && f.NeedItems == 0:
			out.InSync++
		}
	}

	for _, d := range devices {
		if d.DeviceID == status.MyID {
			continue
		}
		c := conns.Connections[d.DeviceID]
		dev := SyncDevice{Name: d.Name, Connected: c.Connected, Paused: d.Paused || c.Paused}
		if seen := stats[d.DeviceID].LastSeen; seen.Year() > 1970 {
			dev.LastSeen = &seen
		}
		if dev.Name == "" {
			dev.Name = strings.SplitN(d.DeviceID, "-", 2)[0]
		}
		if dev.Connected {
			out.Connected++
		}
		out.Devices = append(out.Devices, dev)
	}
	sort.SliceStable(out.Folders, func(i, j int) bool {
		return strings.ToLower(out.Folders[i].Label) < strings.ToLower(out.Folders[j].Label)
	})
	sort.SliceStable(out.Devices, func(i, j int) bool {
		if out.Devices[i].Connected != out.Devices[j].Connected {
			return out.Devices[i].Connected
		}
		return strings.ToLower(out.Devices[i].Name) < strings.ToLower(out.Devices[j].Name)
	})
	return out, nil
}
