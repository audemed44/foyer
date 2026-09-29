package drop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextBecomesLinkWhenItIsOne(t *testing.T) {
	s := NewStore(t.TempDir(), 1<<20)
	cases := []struct {
		text, link, title string
		kind, url, want   string
	}{
		{text: "https://example.com/a", kind: "link", url: "https://example.com/a"},
		{text: "Great read https://example.com/b", kind: "link", url: "https://example.com/b", want: "Great read"},
		{text: "two https://a.com and https://b.com", kind: "text"},
		{text: "just a note", kind: "text"},
		{link: "https://example.com/c", title: "Shared", kind: "link", url: "https://example.com/c", want: "Shared"},
		{text: "line one\nhttps://example.com/d\nline three", kind: "text"},
	}
	for _, c := range cases {
		it, err := s.AddText(c.text, c.link, c.title)
		if err != nil {
			t.Fatalf("%q: %v", c.text, err)
		}
		if it.Kind != c.kind || it.URL != c.url || (c.kind == "link" && it.Title != c.want) {
			t.Errorf("%q → %+v", c.text, it)
		}
	}
	if _, err := s.AddText("  ", "", ""); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty text: %v", err)
	}
	if _, err := s.AddText("", "javascript:alert(1)", ""); err == nil {
		t.Error("accepted a javascript: link")
	}
}

func TestFilesPersistAndDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, 1<<20)
	it, err := s.AddFile("../../etc/My Book.epub", "", strings.NewReader("book"))
	if err != nil {
		t.Fatal(err)
	}
	if it.File.Name != "My Book.epub" || it.File.Size != 4 || it.File.Type != "application/epub+zip" {
		t.Fatalf("file: %+v", it.File)
	}
	// A fresh store reads the same items back.
	again := NewStore(dir, 1<<20)
	got, f, err := again.Open(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if got.ID != it.ID || string(data) != "book" {
		t.Fatalf("reopened: %+v %q", got, data)
	}
	if err := again.Delete(it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", it.ID)); !os.IsNotExist(err) {
		t.Error("file left behind after delete")
	}
	if err := again.Delete(it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestFileLimit(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, 3)
	if _, err := s.AddFile("big.bin", "", strings.NewReader("1234")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "files"))
	if len(entries) != 0 {
		t.Error("partial upload left on disk")
	}
	items, _ := s.List()
	if len(items) != 0 {
		t.Error("item saved for a refused file")
	}
}

func TestSweepRemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, 1<<20)
	keep, _ := s.AddFile("a.txt", "text/plain", strings.NewReader("a"))
	orphan := filepath.Join(dir, "files", "deadbeef")
	os.WriteFile(orphan, []byte("x"), 0o644)
	s.Sweep()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "files", keep.ID)); err != nil {
		t.Error("sweep removed a real file")
	}
}
