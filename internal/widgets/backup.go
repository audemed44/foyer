package widgets

import "time"

// BackupSource is one backed-up thing, from Keep or Kopia, in the terms the
// topology map and the alerts need.
type BackupSource struct {
	// Key identifies the source for alerts.
	Key string `json:"key"`
	// Label names it in messages: Keep's source name, or Kopia's path.
	Label string `json:"label"`
	// Path is where the data is: a host path from Keep, or the path as the
	// backup container sees it (ContainerPaths) from Kopia.
	Path string `json:"path,omitempty"`
	// Volume is a Docker volume the source covers (a database dump).
	Volume string `json:"volume,omitempty"`
	// State is ok, running, stale, errors or never.
	State   string     `json:"state"`
	Last    *time.Time `json:"last,omitempty"`
	Errors  int64      `json:"errors,omitempty"`
	Partial bool       `json:"partial,omitempty"`
}

// BackupData is what Foyer knows about backups, whichever tool runs them.
type BackupData struct {
	Engine  string         `json:"engine"` // the tool: keep or kopia
	Sources []BackupSource `json:"sources"`
	// ContainerPaths means Path is inside the backup tool's container and
	// needs translating through its mounts (Kopia). Keep sends host paths.
	ContainerPaths bool `json:"container_paths"`
}

// AsBackup returns the backup sources in a widget's data, from Keep or
// Kopia.
func AsBackup(data any) (BackupData, bool) {
	switch d := data.(type) {
	case KeepData:
		return d.Backup, true
	case KopiaData:
		out := BackupData{Engine: "kopia", Sources: []BackupSource{}, ContainerPaths: true}
		for _, s := range d.Sources {
			out.Sources = append(out.Sources, BackupSource{
				Key: s.Host + ":" + s.Path, Label: s.Path, Path: s.Path,
				State: s.State, Last: s.Last, Errors: s.Errors,
			})
		}
		return out, true
	}
	return BackupData{}, false
}
