// Package drop is a small shared inbox: text, links and files sent from any
// device (or shared to the installed app) land here until they're deleted.
// Items are kept in <dir>/items.json and file contents in <dir>/files/<id>.
package drop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxText is the most text a single item holds.
const MaxText = 256 << 10

var (
	ErrNotFound = errors.New("no such item")
	ErrTooLarge = errors.New("file is too large")
	ErrEmpty    = errors.New("nothing to save")
)

type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

type Item struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // text | link | file
	Text    string `json:"text,omitempty"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	File    *File  `json:"file,omitempty"`
	Created int64  `json:"created"` // unix milliseconds
}

type Store struct {
	dir     string
	maxFile int64

	mu    sync.Mutex
	items []Item // newest first; loaded lazily
	ready bool
}

// NewStore keeps the inbox in dir. Files larger than maxFile bytes are refused.
func NewStore(dir string, maxFile int64) *Store {
	return &Store{dir: dir, maxFile: maxFile}
}

func (s *Store) MaxFile() int64 { return s.maxFile }

func (s *Store) load() error {
	if s.ready {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "items.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.items = []Item{}
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &s.items); err != nil {
			return fmt.Errorf("drop/items.json: %w", err)
		}
	}
	s.ready = true
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "items.json")
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// List returns the items, newest first.
func (s *Store) List() ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return append([]Item{}, s.items...), nil
}

func (s *Store) Get(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Item{}, err
	}
	for _, it := range s.items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, ErrNotFound
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) add(it Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Item{}, err
	}
	s.items = append([]Item{it}, s.items...)
	if err := s.save(); err != nil {
		s.items = s.items[1:]
		return Item{}, err
	}
	return it, nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

// AddText saves a note, or a link when the text is (or ends with) a single
// URL, which is how most apps share a page: "Page title https://…".
func (s *Store) AddText(text, link, title string) (Item, error) {
	text, link, title = strings.TrimSpace(text), strings.TrimSpace(link), strings.TrimSpace(title)
	if len(text) > MaxText {
		return Item{}, ErrTooLarge
	}
	if link == "" {
		if found := urlPattern.FindAllString(text, -1); len(found) == 1 {
			rest := strings.TrimSpace(strings.Replace(text, found[0], "", 1))
			// Only a link and at most a short caption; longer text stays a note.
			if utf8.RuneCountInString(rest) <= 200 && !strings.Contains(rest, "\n") {
				link = found[0]
				if title == "" {
					title = rest
				}
				text = ""
			}
		}
	}
	if link != "" {
		if u, err := url.Parse(link); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Item{}, fmt.Errorf("invalid link")
		}
		return s.add(Item{
			ID: newID(), Kind: "link", URL: link, Title: clip(title, 300), Text: text,
			Created: time.Now().UnixMilli(),
		})
	}
	if text == "" {
		return Item{}, ErrEmpty
	}
	return s.add(Item{ID: newID(), Kind: "text", Text: text, Title: clip(title, 300), Created: time.Now().UnixMilli()})
}

// AddFile streams r to disk and saves it as an item.
func (s *Store) AddFile(name, contentType string, r io.Reader) (Item, error) {
	name = cleanName(name)
	contentType = fileType(name, contentType)
	files := filepath.Join(s.dir, "files")
	if err := os.MkdirAll(files, 0o755); err != nil {
		return Item{}, err
	}
	id := newID()
	path := filepath.Join(files, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return Item{}, err
	}
	n, err := io.Copy(f, io.LimitReader(r, s.maxFile+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > s.maxFile {
		err = ErrTooLarge
	}
	if err != nil {
		os.Remove(path)
		return Item{}, err
	}
	item, err := s.add(Item{
		ID: id, Kind: "file", File: &File{Name: name, Size: n, Type: contentType},
		Created: time.Now().UnixMilli(),
	})
	if err != nil {
		os.Remove(path)
	}
	return item, err
}

// Open returns a file item's contents.
func (s *Store) Open(id string) (Item, *os.File, error) {
	it, err := s.Get(id)
	if err != nil {
		return Item{}, nil, err
	}
	if it.File == nil {
		return Item{}, nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.dir, "files", it.ID))
	if err != nil {
		return Item{}, nil, ErrNotFound
	}
	return it, f, nil
}

// SetTitle fills in a link's page title once it has been fetched.
func (s *Store) SetTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Title = clip(title, 300)
			return s.save()
		}
	}
	return ErrNotFound
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	for i, it := range s.items {
		if it.ID != id {
			continue
		}
		s.items = append(s.items[:i:i], s.items[i+1:]...)
		if err := s.save(); err != nil {
			return err
		}
		if it.File != nil {
			os.Remove(filepath.Join(s.dir, "files", it.ID))
		}
		return nil
	}
	return ErrNotFound
}

// Usage is the total size of stored files.
func (s *Store) Usage() int64 {
	items, _ := s.List()
	var n int64
	for _, it := range items {
		if it.File != nil {
			n += it.File.Size
		}
	}
	return n
}

// Sweep removes files that no item refers to, left behind by a crash.
func (s *Store) Sweep() {
	items, err := s.List()
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, it := range items {
		known[it.ID] = true
	}
	entries, _ := os.ReadDir(filepath.Join(s.dir, "files"))
	for _, e := range entries {
		if !known[e.Name()] {
			os.Remove(filepath.Join(s.dir, "files", e.Name()))
		}
	}
}

// types covers what the runtime image has no mime.types file for.
var types = map[string]string{
	".epub": "application/epub+zip", ".pdf": "application/pdf", ".mobi": "application/x-mobipocket-ebook",
	".azw3": "application/vnd.amazon.ebook", ".cbz": "application/vnd.comicbook+zip",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".json": "application/json",
	".zip": "application/zip", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".heic": "image/heic",
	".svg": "image/svg+xml", ".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".ogg": "audio/ogg", ".html": "text/html",
}

// fileType is a file's bare MIME type: the browser's when it sent a useful
// one, else a guess from the extension.
func fileType(name, sent string) string {
	if t, _, err := mime.ParseMediaType(sent); err == nil && t != "application/octet-stream" {
		return strings.ToLower(t)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := types[ext]; ok {
		return t
	}
	if t, _, err := mime.ParseMediaType(mime.TypeByExtension(ext)); err == nil {
		return t
	}
	return "application/octet-stream"
}

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return clip(name, 200)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
