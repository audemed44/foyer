package server

import (
	"strings"
	"testing"
	"time"

	"github.com/audemed44/foyer/internal/widgets"
)

func TestPublicConfigHidesAlertTarget(t *testing.T) {
	h, store := setup(t)
	cfg := store.Config()
	cfg.Alerts.AppriseURL = "http://apprise:8000/notify/s3cretkey"
	if _, err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if body := do(h, "GET", "/api/config", "").Body.String(); strings.Contains(body, "s3cretkey") {
		t.Fatalf("public config leaks the Apprise URL: %s", body)
	}
	if body := do(h, "GET", "/api/alerts", "").Body.String(); !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("alerts: %s", body)
	}
}

func TestIntegrationProblems(t *testing.T) {
	last := time.Now().Add(-72 * time.Hour)
	backups := backupProblems(widgets.KopiaData{Sources: []widgets.KopiaSource{
		{Host: "h", Path: "/data", State: "stale", Last: &last},
		{Host: "h", Path: "/ok", State: "ok"},
		{Host: "h", Path: "/new", State: "never"},
	}})
	if len(backups) != 2 || backups[0].Title != "The backup of /data is overdue" || backups[1].Key != "backup:h:/new" {
		t.Fatalf("backups: %+v", backups)
	}
	keep := backupProblems(widgets.KeepData{Backup: widgets.BackupData{Engine: "keep", Sources: []widgets.BackupSource{
		{Key: "keep:ledger", Label: "ledger", State: "errors"},
		{Key: "keep:lookout", Label: "lookout", State: "never"},
	}}})
	if len(keep) != 2 || keep[0].Key != "backup:keep:ledger" || keep[0].Title != "The backup of ledger had problems" ||
		keep[1].Body != "Keep has no good backup of it." {
		t.Fatalf("keep: %+v", keep)
	}

	syncs := syncProblems(widgets.SyncthingData{Folders: []widgets.SyncFolder{
		{ID: "a", Label: "Photos", State: "error", Error: "folder marker missing"},
		{ID: "b", Label: "Books", State: "idle", PullErrors: 3},
		{ID: "c", Label: "Fine", State: "idle"},
	}})
	if len(syncs) != 2 || syncs[1].Body != "3 files couldn't sync." {
		t.Fatalf("sync: %+v", syncs)
	}

	now := time.Now()
	certs := certProblems(widgets.NPMData{WarnDays: 14, Certificates: []widgets.Certificate{
		{Name: "soon", Days: 5, Hosts: 1, Expires: now.Add(5 * 24 * time.Hour)},
		{Name: "gone", Days: -1, Hosts: 2, Expires: now.Add(-24 * time.Hour)},
		{Name: "unused", Days: 1, Hosts: 0, Expires: now.Add(24 * time.Hour)},
		{Name: "later", Days: 60, Hosts: 1, Expires: now.Add(60 * 24 * time.Hour)},
	}})
	if len(certs) != 2 || certs[0].Level != "warning" || certs[1].Level != "failure" {
		t.Fatalf("certs: %+v", certs)
	}
}
