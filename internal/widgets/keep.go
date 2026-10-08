package widgets

import (
	"context"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

// KeepData is Keep's own card (with its Run now button) plus its sources,
// which feed the topology map and the backup alerts as Kopia's did.
type KeepData struct {
	AppWidget
	Backup BackupData `json:"backup"`
}

// keep reads Keep's card and its read-only backup report.
// Settings: url (e.g. http://keep:8080), key (KEEP_TOKEN).
func keep(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "key"); err != nil {
		return nil, err
	}
	base := w.String("url")
	auth := []string{"Authorization", "Bearer " + w.String("key")}
	var out KeepData
	if err := getJSON(ctx, join(base, WellKnownPath), &out.AppWidget, auth...); err != nil {
		return nil, err
	}
	if err := out.AppWidget.sanitize(); err != nil {
		return nil, err
	}
	var raw struct {
		Engine  string `json:"engine"`
		Sources []struct {
			Name    string     `json:"name"`
			Path    string     `json:"path"`
			Volume  string     `json:"volume"`
			State   string     `json:"state"`
			Last    *time.Time `json:"last"`
			Partial bool       `json:"partial"`
		} `json:"sources"`
	}
	if err := getJSON(ctx, join(base, "/api/foyer/backups"), &raw, auth...); err != nil {
		return nil, err
	}
	out.Backup = BackupData{Engine: "keep", Sources: make([]BackupSource, 0, len(raw.Sources))}
	for _, s := range raw.Sources {
		out.Backup.Sources = append(out.Backup.Sources, BackupSource{
			Key: "keep:" + s.Name, Label: s.Name, Path: s.Path, Volume: s.Volume,
			State: s.State, Last: s.Last, Partial: s.Partial,
		})
	}
	return out, nil
}
