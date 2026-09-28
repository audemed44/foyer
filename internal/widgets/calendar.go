package widgets

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

type Event struct {
	Title  string `json:"title"`
	Start  string `json:"start"` // RFC 3339, or YYYY-MM-DD for all-day events
	AllDay bool   `json:"all_day"`
	start  time.Time
}

// calendar lists upcoming events from an iCal feed.
// Settings: url, days (lookahead, default 30), max_events (default 8).
func calendar(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.String("url"), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s answered HTTP %d", req.URL.Host, resp.StatusCode)
	}
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, w.Int("days", 30))
	events := ParseICal(io.LimitReader(resp.Body, 8<<20), from, to)
	if n := w.Int("max_events", 8); len(events) > n {
		events = events[:n]
	}
	return map[string]any{"events": events}, nil
}

// ParseICal returns the events starting in [from, to), sorted by start.
// It covers what feeds from self-hosted apps use: DTSTART with UTC, TZID or
// floating times, all-day dates, and simple RRULEs (FREQ, INTERVAL, COUNT,
// UNTIL).
func ParseICal(r io.Reader, from, to time.Time) []Event {
	var events []Event
	var props map[string]prop
	for _, line := range unfold(r) {
		switch {
		case line == "BEGIN:VEVENT":
			props = map[string]prop{}
		case line == "END:VEVENT":
			if props != nil {
				events = append(events, expand(props, from, to)...)
			}
			props = nil
		case props != nil:
			if p, ok := parseProp(line); ok {
				props[p.name] = p
			}
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].start.Before(events[j].start) })
	if events == nil {
		events = []Event{}
	}
	return events
}

type prop struct {
	name   string
	params map[string]string
	value  string
}

func unfold(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func parseProp(line string) (prop, bool) {
	colon := strings.Index(line, ":")
	if colon < 0 {
		return prop{}, false
	}
	head, value := line[:colon], line[colon+1:]
	parts := strings.Split(head, ";")
	p := prop{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: value}
	for _, param := range parts[1:] {
		if k, v, ok := strings.Cut(param, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return p, true
}

func parseTime(p prop) (time.Time, bool, bool) {
	v := p.value
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err == nil
	}
	loc := time.Local
	if tz := p.params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	return t, false, err == nil
}

func unescape(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

func expand(props map[string]prop, from, to time.Time) []Event {
	startProp, ok := props["DTSTART"]
	if !ok {
		return nil
	}
	start, allDay, ok := parseTime(startProp)
	if !ok {
		return nil
	}
	title := unescape(props["SUMMARY"].value)
	newEvent := func(t time.Time) Event {
		e := Event{Title: title, AllDay: allDay, start: t}
		if allDay {
			e.Start = t.Format("2006-01-02")
		} else {
			e.Start = t.Format(time.RFC3339)
		}
		return e
	}
	// An event already running counts until it ends.
	end := start
	if endProp, ok := props["DTEND"]; ok {
		if t, _, ok := parseTime(endProp); ok {
			end = t
		}
	}
	duration := end.Sub(start)
	inRange := func(t time.Time) bool {
		return t.Before(to) && (!t.Before(from) || t.Add(duration).After(from))
	}

	rule, hasRule := props["RRULE"]
	if !hasRule {
		if inRange(start) {
			return []Event{newEvent(start)}
		}
		return nil
	}
	fields := map[string]string{}
	for _, part := range strings.Split(rule.value, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			fields[strings.ToUpper(k)] = v
		}
	}
	interval, _ := strconv.Atoi(fields["INTERVAL"])
	interval = max(interval, 1)
	count, _ := strconv.Atoi(fields["COUNT"])
	var until time.Time
	if u := fields["UNTIL"]; u != "" {
		until, _, _ = parseTime(prop{value: u, params: map[string]string{}})
	}
	step := map[string]func(time.Time, int) time.Time{
		"DAILY":   func(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) },
		"WEEKLY":  func(t time.Time, n int) time.Time { return t.AddDate(0, 0, 7*n) },
		"MONTHLY": func(t time.Time, n int) time.Time { return t.AddDate(0, n, 0) },
		"YEARLY":  func(t time.Time, n int) time.Time { return t.AddDate(n, 0, 0) },
	}[fields["FREQ"]]
	if step == nil {
		if inRange(start) {
			return []Event{newEvent(start)}
		}
		return nil
	}
	var out []Event
	for i := 0; i < 5000; i++ {
		t := step(start, i*interval)
		if (count > 0 && i >= count) || (!until.IsZero() && t.After(until)) || !t.Before(to) {
			break
		}
		if inRange(t) {
			out = append(out, newEvent(t))
		}
	}
	return out
}
