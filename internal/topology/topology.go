// Package topology maps how the homelab fits together: which domain (an
// Nginx Proxy Manager host) reaches which container, and where each
// container keeps its data — and whether Kopia backs that data up or
// Syncthing syncs it.
package topology

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/docker"
	"github.com/audemed44/foyer/internal/widgets"
)

// Ref is the dashboard service shown for a node, for its name and icon.
type Ref struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	URL  string `json:"url,omitempty"`
}

type Domain struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Forward  string   `json:"forward"`
	SSL      bool     `json:"ssl"`
	CertDays *int     `json:"cert_days,omitempty"`
	Enabled  bool     `json:"enabled"`
	Error    string   `json:"error,omitempty"`
	// Target is the node the proxy forwards to: a container, "host", or
	// empty when nothing answers at that address.
	Target  string `json:"target,omitempty"`
	Service *Ref   `json:"service,omitempty"`
}

type Container struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Image     string           `json:"image"`
	State     string           `json:"state"`
	Health    string           `json:"health,omitempty"`
	Status    string           `json:"status"`
	Project   string           `json:"project,omitempty"`
	Published []docker.PortMap `json:"published,omitempty"`
	Networks  []string         `json:"networks,omitempty"`
	Service   *Ref             `json:"service,omitempty"`
}

type Backup struct {
	State string     `json:"state"` // a Kopia source state: ok, stale, errors, …
	Last  *time.Time `json:"last,omitempty"`
	// Source is the snapshot path as Kopia sees it.
	Source string `json:"source"`
	// Partial means only part of this folder is in the snapshot.
	Partial bool `json:"partial,omitempty"`
}

type Sync struct {
	Folder  string `json:"folder"`
	State   string `json:"state"`
	Partial bool   `json:"partial,omitempty"`
}

type Storage struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // bind | volume
	// Class separates app data from plumbing: data, socket (the Docker
	// socket) or system (/etc/localtime and friends).
	Class  string  `json:"class"`
	Path   string  `json:"path"` // host path
	Name   string  `json:"name,omitempty"`
	Backup *Backup `json:"backup,omitempty"`
	Sync   *Sync   `json:"sync,omitempty"`
	// Written is set when a running app (other than Kopia) writes here.
	Written bool `json:"written,omitempty"`
}

type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Label is the port a domain forwards to, or where a mount appears
	// inside the container.
	Label    string `json:"label,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type Issue struct {
	Tone string `json:"tone"` // bad | warn
	Text string `json:"text"`
	Node string `json:"node,omitempty"`
}

type Host struct {
	ID    string `json:"id"`
	Ports []int  `json:"ports"`
}

type Graph struct {
	Domains    []Domain    `json:"domains"`
	Containers []Container `json:"containers"`
	Host       *Host       `json:"host,omitempty"`
	Storage    []Storage   `json:"storage"`
	Links      []Link      `json:"links"`
	Issues     []Issue     `json:"issues"`
	Sources    Sources     `json:"sources"`
}

// Sources says what the map was built from, with the error for any source
// that's configured but failed ("" when fine, absent when not configured).
type Sources struct {
	Docker    *string `json:"docker,omitempty"`
	NPM       *string `json:"npm,omitempty"`
	Kopia     *string `json:"kopia,omitempty"`
	Syncthing *string `json:"syncthing,omitempty"`
}

// Input is everything Build needs; any part may be missing.
type Input struct {
	Config     config.Config
	Containers []docker.Container
	Details    map[string]docker.Details // by container id
	NPM        *widgets.NPMData
	Kopia      *widgets.KopiaData
	// KopiaContainer and SyncthingContainer are the containers the tools
	// run in, to translate their paths into host paths.
	KopiaContainer     string
	Syncthing          *widgets.SyncthingData
	SyncthingContainer string
	Sources            Sources
}

// localHosts are forward targets that mean "a port on this machine".
var localHosts = []string{"host.docker.internal", "localhost", "127.0.0.1", "::1", "host-gateway"}

// Resolve finds where a proxy host forwards to: the container it names (by
// container name, network alias or compose service), or the container that
// publishes that port on this machine. host is true when the address is
// this machine but no container publishes the port (a process on the host).
func Resolve(forwardHost string, port int, containers []docker.Container, details map[string]docker.Details) (name string, host bool) {
	h := strings.ToLower(strings.TrimSpace(forwardHost))
	for _, c := range containers {
		if strings.ToLower(c.Name) == h || strings.ToLower(c.Labels["com.docker.compose.service"]) == h {
			return c.Name, false
		}
	}
	for _, c := range containers {
		for _, a := range details[c.ID].Aliases {
			if strings.ToLower(a) == h {
				return c.Name, false
			}
		}
	}
	if slices.Contains(localHosts, h) || net.ParseIP(h) != nil {
		for _, c := range containers {
			for _, p := range c.Published {
				if p.Host == port && c.State == "running" {
					return c.Name, false
				}
			}
		}
		return "", true
	}
	return "", false
}

// hostPath translates a path inside a container into a host path, through
// the container's mounts. ok is false if no mount covers it.
func hostPath(p string, mounts []docker.Mount) (string, bool) {
	best := -1
	for i, m := range mounts {
		if within(p, m.Destination) && (best < 0 || len(m.Destination) > len(mounts[best].Destination)) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	m := mounts[best]
	return path.Join(m.Source, strings.TrimPrefix(p, m.Destination)), true
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	p, dir = path.Clean(p), path.Clean(dir)
	return p == dir || dir == "/" || strings.HasPrefix(p, dir+"/")
}

func storageClass(m docker.Mount) string {
	src := m.Source
	if strings.HasSuffix(src, "/docker.sock") {
		return "socket"
	}
	for _, sys := range []string{"/etc", "/sys", "/proc", "/dev", "/usr", "/lib", "/run", "/var/run", "/boot"} {
		if within(src, sys) {
			return "system"
		}
	}
	return "data"
}

func storageID(m docker.Mount) string {
	if m.Type == "volume" && m.Name != "" {
		return "v:" + m.Name
	}
	return "p:" + m.Source
}

// serviceRefs indexes dashboard services by container name and domain.
func serviceRefs(cfg config.Config) (byContainer, byDomain map[string]*Ref) {
	byContainer, byDomain = map[string]*Ref{}, map[string]*Ref{}
	for _, s := range cfg.Services() {
		ref := &Ref{Name: s.Name, Icon: s.Icon, URL: s.URL}
		if u, err := url.Parse(s.URL); err == nil && u.Hostname() != "" {
			if _, seen := byDomain[strings.ToLower(u.Hostname())]; !seen {
				byDomain[strings.ToLower(u.Hostname())] = ref
			}
		}
		name := s.Container
		if name == "" {
			if u, err := url.Parse(config.ExpandEnv(s.Ping)); err == nil {
				name = u.Hostname()
			}
		}
		if _, seen := byContainer[name]; name != "" && !seen {
			byContainer[name] = ref
		}
	}
	return byContainer, byDomain
}

func Build(in Input) Graph {
	g := Graph{
		Domains: []Domain{}, Containers: []Container{}, Storage: []Storage{},
		Links: []Link{}, Issues: []Issue{}, Sources: in.Sources,
	}
	byContainer, byDomain := serviceRefs(in.Config)
	byName := map[string]docker.Container{}
	for _, c := range in.Containers {
		byName[c.Name] = c
		d := in.Details[c.ID]
		g.Containers = append(g.Containers, Container{
			ID: "c:" + c.Name, Name: c.Name, Image: c.Image, State: c.State, Health: c.Health,
			Status: c.Status, Project: c.Project, Published: c.Published, Networks: d.Networks,
			Service: byContainer[c.Name],
		})
	}

	// Domains → containers.
	var hostPorts []int
	if in.NPM != nil {
		certs := map[string]widgets.Certificate{}
		for _, c := range in.NPM.Certificates {
			certs[c.Name] = c
		}
		for _, h := range in.NPM.Hosts {
			if len(h.Domains) == 0 {
				continue
			}
			d := Domain{
				ID: "d:" + h.Domains[0], Name: h.Domains[0], Aliases: h.Domains[1:], SSL: h.SSL,
				Enabled: h.Enabled, Error: h.Error, Service: byDomain[strings.ToLower(h.Domains[0])],
				Forward: fmt.Sprintf("%s://%s:%d", h.Scheme, h.ForwardHost, h.ForwardPort),
			}
			if c, ok := certs[h.Certificate]; ok && h.SSL && !c.Expires.IsZero() {
				days := c.Days
				d.CertDays = &days
			}
			name, onHost := Resolve(h.ForwardHost, h.ForwardPort, in.Containers, in.Details)
			label := fmt.Sprintf(":%d", h.ForwardPort)
			switch {
			case name != "":
				d.Target = "c:" + name
				if d.Service == nil {
					d.Service = byContainer[name]
				}
				if c := byName[name]; c.State != "running" && d.Enabled {
					g.Issues = append(g.Issues, Issue{Tone: "bad", Node: d.ID,
						Text: fmt.Sprintf("%s forwards to %s, which is %s", d.Name, name, c.State)})
				}
			case onHost:
				d.Target = "host"
				if !slices.Contains(hostPorts, h.ForwardPort) {
					hostPorts = append(hostPorts, h.ForwardPort)
				}
			case d.Enabled && in.Containers != nil:
				g.Issues = append(g.Issues, Issue{Tone: "bad", Node: d.ID,
					Text: fmt.Sprintf("%s forwards to %s:%d, but no container answers to that name", d.Name, h.ForwardHost, h.ForwardPort)})
			}
			if d.Error != "" {
				g.Issues = append(g.Issues, Issue{Tone: "bad", Node: d.ID, Text: d.Name + ": " + d.Error})
			}
			if d.CertDays != nil && *d.CertDays < in.NPM.WarnDays {
				tone := "warn"
				if *d.CertDays < 3 {
					tone = "bad"
				}
				g.Issues = append(g.Issues, Issue{Tone: tone, Node: d.ID,
					Text: fmt.Sprintf("The certificate for %s expires in %d days", d.Name, *d.CertDays)})
			}
			if d.Target != "" {
				g.Links = append(g.Links, Link{From: d.ID, To: d.Target, Label: label})
			}
			g.Domains = append(g.Domains, d)
		}
		sort.SliceStable(g.Domains, func(i, j int) bool { return g.Domains[i].Name < g.Domains[j].Name })
	}
	if len(hostPorts) > 0 {
		sort.Ints(hostPorts)
		g.Host = &Host{ID: "host", Ports: hostPorts}
	}

	// Where Kopia and Syncthing see their paths, on the host.
	type backed struct {
		host string
		src  widgets.KopiaSource
	}
	var backups []backed
	if in.Kopia != nil {
		mounts := in.Details[byName[in.KopiaContainer].ID].Mounts
		for _, s := range in.Kopia.Sources {
			if p, ok := hostPath(s.Path, mounts); ok {
				backups = append(backups, backed{p, s})
			} else if in.KopiaContainer == "" {
				// Kopia on the host itself: its paths are host paths.
				backups = append(backups, backed{s.Path, s})
			}
		}
	}
	type synced struct {
		host   string
		folder widgets.SyncFolder
	}
	var syncs []synced
	if in.Syncthing != nil {
		mounts := in.Details[byName[in.SyncthingContainer].ID].Mounts
		for _, f := range in.Syncthing.Folders {
			if p, ok := hostPath(f.Path, mounts); ok {
				syncs = append(syncs, synced{p, f})
			} else if in.SyncthingContainer == "" {
				syncs = append(syncs, synced{f.Path, f})
			}
		}
	}

	// Containers → storage.
	seen := map[string]bool{}
	written := map[string]bool{} // storage a running app (other than Kopia) writes to
	for _, c := range in.Containers {
		for _, m := range in.Details[c.ID].Mounts {
			if m.Type != "bind" && m.Type != "volume" {
				continue
			}
			id := storageID(m)
			g.Links = append(g.Links, Link{From: "c:" + c.Name, To: id, Label: m.Destination, ReadOnly: !m.RW})
			if m.RW && c.State == "running" && c.Name != in.KopiaContainer {
				written[id] = true
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			st := Storage{ID: id, Kind: m.Type, Class: storageClass(m), Path: m.Source, Name: m.Name}
			for _, b := range backups {
				switch {
				case within(st.Path, b.host):
					st.Backup = better(st.Backup, &Backup{State: b.src.State, Last: b.src.Last, Source: b.src.Path})
				case within(b.host, st.Path):
					st.Backup = better(st.Backup, &Backup{State: b.src.State, Last: b.src.Last, Source: b.src.Path, Partial: true})
				}
			}
			// Either the storage is inside a synced folder, or synced
			// folders are inside it (then it's partly synced).
			var inside []widgets.SyncFolder
			for _, s := range syncs {
				if within(st.Path, s.host) {
					st.Sync, inside = &Sync{Folder: s.folder.Label, State: s.folder.State}, nil
					break
				}
				if within(s.host, st.Path) {
					inside = append(inside, s.folder)
				}
			}
			if len(inside) > 0 {
				label := inside[0].Label
				if len(inside) > 1 {
					label = fmt.Sprintf("%d folders", len(inside))
				}
				st.Sync = &Sync{Folder: label, State: inside[0].State, Partial: true}
			}
			g.Storage = append(g.Storage, st)
		}
	}
	for i := range g.Storage {
		g.Storage[i].Written = written[g.Storage[i].ID]
	}
	if in.Kopia != nil {
		n := 0
		for _, st := range g.Storage {
			if st.Class == "data" && written[st.ID] && (st.Backup == nil || st.Backup.Partial) {
				n++
			}
		}
		if n > 0 {
			noun := "folders"
			if n == 1 {
				noun = "folder"
			}
			g.Issues = append(g.Issues, Issue{Tone: "warn",
				Text: fmt.Sprintf("%d data %s written by running containers aren't fully in any Kopia snapshot", n, noun)})
		}
	}
	for _, st := range g.Storage {
		if st.Backup != nil && !st.Backup.Partial && (st.Backup.State == "stale" || st.Backup.State == "never") {
			g.Issues = append(g.Issues, Issue{Tone: "bad", Node: st.ID,
				Text: fmt.Sprintf("The backup of %s is %s", st.Path, st.Backup.State)})
		}
	}
	sort.SliceStable(g.Issues, func(i, j int) bool { return g.Issues[i].Tone == "bad" && g.Issues[j].Tone != "bad" })
	return g
}

// better keeps the more complete (then the more recent) backup match.
func better(cur, next *Backup) *Backup {
	switch {
	case cur == nil:
		return next
	case cur.Partial != next.Partial:
		if next.Partial {
			return cur
		}
		return next
	case next.Last != nil && (cur.Last == nil || next.Last.After(*cur.Last)):
		return next
	}
	return cur
}
