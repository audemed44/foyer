package widgets

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type KopiaSource struct {
	Path   string     `json:"path"`
	Host   string     `json:"host"`
	User   string     `json:"user"`
	Status string     `json:"status"` // Kopia's: IDLE, PENDING, UPLOADING, PAUSED, …
	Last   *time.Time `json:"last,omitempty"`
	// Seconds the last snapshot took.
	Duration float64    `json:"duration,omitempty"`
	Size     int64      `json:"size"`
	Files    int64      `json:"files"`
	Errors   int64      `json:"errors"`
	Next     *time.Time `json:"next,omitempty"`
	// State is Foyer's verdict: ok, running, stale (older than stale_hours or
	// overdue), errors (the last snapshot skipped files) or never.
	State string `json:"state"`
}

type KopiaData struct {
	Sources    []KopiaSource `json:"sources"`
	TotalSize  int64         `json:"total_size"`
	Latest     *time.Time    `json:"latest,omitempty"`
	Problems   int           `json:"problems"`
	StaleHours int           `json:"stale_hours"`
}

var csrfMeta = regexp.MustCompile(`name="kopia-csrf-token" content="([0-9a-fA-F]+)"`)

// kopia reads snapshot sources from a Kopia server (`kopia server start`).
// Settings: url, username, password, stale_hours (default 48).
//
// The API behind Kopia's UI wants a session cookie and the CSRF token its
// page embeds, so Foyer loads the page first, like a browser would.
func kopia(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "username", "password"); err != nil {
		return nil, err
	}
	base, user, pass := w.String("url"), w.String("username"), w.String("password")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, join(base, "/"), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s", req.URL.Host)
	}
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%s rejected the credentials (HTTP %d)", req.URL.Host, resp.StatusCode)
	}
	m := csrfMeta.FindSubmatch(page)
	var session string
	for _, c := range resp.Cookies() {
		if c.Name == "Kopia-Session-Cookie" {
			session = c.Value
		}
	}
	if m == nil || session == "" {
		return nil, fmt.Errorf("%s doesn't look like a Kopia server", req.URL.Host)
	}

	var raw struct {
		Sources []struct {
			Source struct {
				Host     string `json:"host"`
				UserName string `json:"userName"`
				Path     string `json:"path"`
			} `json:"source"`
			Status       string `json:"status"`
			LastSnapshot *struct {
				StartTime time.Time `json:"startTime"`
				EndTime   time.Time `json:"endTime"`
				Stats     struct {
					TotalSize  int64 `json:"totalSize"`
					FileCount  int64 `json:"fileCount"`
					ErrorCount int64 `json:"errorCount"`
				} `json:"stats"`
				RootEntry struct {
					Summ struct {
						Size      int64 `json:"size"`
						Files     int64 `json:"files"`
						NumFailed int64 `json:"numFailed"`
					} `json:"summ"`
				} `json:"rootEntry"`
			} `json:"lastSnapshot"`
			NextSnapshotTime *time.Time `json:"nextSnapshotTime"`
		} `json:"sources"`
	}
	auth := "Basic " + basicAuth(user, pass)
	err = getJSON(ctx, join(base, "/api/v1/sources"), &raw,
		"Authorization", auth,
		"Cookie", "Kopia-Session-Cookie="+session,
		"X-Kopia-Csrf-Token", string(m[1]))
	if err != nil {
		return nil, err
	}

	staleHours := max(1, w.Int("stale_hours", 48))
	now := time.Now()
	out := KopiaData{Sources: []KopiaSource{}, StaleHours: staleHours}
	for _, s := range raw.Sources {
		src := KopiaSource{
			Path: s.Source.Path, Host: s.Source.Host, User: s.Source.UserName,
			Status: s.Status, Next: s.NextSnapshotTime,
		}
		if snap := s.LastSnapshot; snap != nil {
			end := snap.EndTime
			src.Last = &end
			src.Duration = snap.EndTime.Sub(snap.StartTime).Seconds()
			src.Size = max(snap.Stats.TotalSize, snap.RootEntry.Summ.Size)
			src.Files = max(snap.Stats.FileCount, snap.RootEntry.Summ.Files)
			src.Errors = max(snap.Stats.ErrorCount, snap.RootEntry.Summ.NumFailed)
			out.TotalSize += src.Size
			if out.Latest == nil || end.After(*out.Latest) {
				out.Latest = &end
			}
		}
		src.State = kopiaState(src, now, staleHours)
		if src.State != "ok" && src.State != "running" {
			out.Problems++
		}
		out.Sources = append(out.Sources, src)
	}
	sort.SliceStable(out.Sources, func(i, j int) bool {
		pi, pj := kopiaRank[out.Sources[i].State], kopiaRank[out.Sources[j].State]
		if pi != pj {
			return pi < pj
		}
		return out.Sources[i].Path < out.Sources[j].Path
	})
	return out, nil
}

var kopiaRank = map[string]int{"never": 0, "stale": 1, "errors": 2, "running": 3, "ok": 4}

func kopiaState(s KopiaSource, now time.Time, staleHours int) string {
	switch {
	case s.Status == "UPLOADING":
		return "running"
	case s.Last == nil:
		return "never"
	case now.Sub(*s.Last) > time.Duration(staleHours)*time.Hour:
		return "stale"
	// A scheduled snapshot more than an hour late didn't run.
	case s.Next != nil && now.Sub(*s.Next) > time.Hour:
		return "stale"
	case s.Errors > 0:
		return "errors"
	}
	return "ok"
}
