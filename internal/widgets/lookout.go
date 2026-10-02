package widgets

import (
	"context"

	"github.com/audemed44/foyer/internal/config"
)

// LookoutCheck is one of Lookout's checks, as far as Foyer cares: what it
// watches and how it's doing.
type LookoutCheck struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`   // http, tcp, docker, …
	Target    string  `json:"target"` // a URL, host:port or container name
	Status    string  `json:"status"` // up, down, pending, asleep, paused, maintenance, unknown, running
	Message   string  `json:"message,omitempty"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
}

// LookoutData is Lookout's own card plus its checks, which become the
// services' status when Lookout is set up (it's the monitor; Foyer then
// stops pinging what Lookout watches).
type LookoutData struct {
	AppWidget
	Checks []LookoutCheck `json:"checks"`
}

// lookout reads Lookout's card and its checks.
// Settings: url (e.g. http://lookout:8080), key (LOOKOUT_TOKEN).
func lookout(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url", "key"); err != nil {
		return nil, err
	}
	base := w.String("url")
	auth := []string{"Authorization", "Bearer " + w.String("key")}
	var out LookoutData
	if err := getJSON(ctx, join(base, WellKnownPath), &out.AppWidget, auth...); err != nil {
		return nil, err
	}
	if err := out.AppWidget.sanitize(); err != nil {
		return nil, err
	}
	var rows []struct {
		Check struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Target string `json:"target"`
		} `json:"check"`
		Status string `json:"status"`
		State  struct {
			Message string  `json:"message"`
			Latency float64 `json:"latency"`
		} `json:"state"`
	}
	if err := getJSON(ctx, join(base, "/api/checks"), &rows, auth...); err != nil {
		return nil, err
	}
	out.Checks = make([]LookoutCheck, 0, len(rows))
	for _, r := range rows {
		out.Checks = append(out.Checks, LookoutCheck{
			Name: r.Check.Name, Type: r.Check.Type, Target: r.Check.Target,
			Status: r.Status, Message: r.State.Message, LatencyMS: r.State.Latency,
		})
	}
	return out, nil
}

// AsLookout returns Lookout's checks from a widget's data.
func AsLookout(data any) (LookoutData, bool) {
	d, ok := data.(LookoutData)
	return d, ok
}
