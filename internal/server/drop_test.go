package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/audemed44/foyer/internal/config"
	"github.com/audemed44/foyer/internal/drop"
	"github.com/audemed44/foyer/internal/monitor"
)

func form(t *testing.T, fields map[string]string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for name, body := range files {
		fw, _ := mw.CreateFormFile("files", name)
		io.WriteString(fw, body)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func post(h http.Handler, path string, body io.Reader, ct string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, body)
	req.Header.Set("Content-Type", ct)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDropRoundTrip(t *testing.T) {
	h, _ := setup(t)
	body, ct := form(t, map[string]string{"text": "hello"}, map[string]string{"notes.txt": "file body"})
	rec := post(h, "/api/drop", body, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post: %d %s", rec.Code, rec.Body)
	}
	var created []drop.Item
	json.Unmarshal(rec.Body.Bytes(), &created)
	if len(created) != 2 {
		t.Fatalf("created: %+v", created)
	}

	var list dropResponse
	json.Unmarshal(do(h, "GET", "/api/drop", "").Body.Bytes(), &list)
	if len(list.Items) != 2 || list.Usage != 9 || list.MaxFile <= 0 {
		t.Fatalf("list: %+v", list)
	}
	var file drop.Item
	for _, it := range list.Items {
		if it.Kind == "file" {
			file = it
		}
	}
	rec = do(h, "GET", "/api/drop/"+file.ID+"/file", "")
	if rec.Body.String() != "file body" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("file: %q %v", rec.Body, rec.Header())
	}
	if rec := do(h, "DELETE", "/api/drop/"+file.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestDropServesActiveContentAsDownload(t *testing.T) {
	h, _ := setup(t)
	body, ct := form(t, nil, map[string]string{"page.html": "<script>alert(1)</script>"})
	rec := post(h, "/api/drop", body, ct)
	var created []drop.Item
	json.Unmarshal(rec.Body.Bytes(), &created)
	rec = do(h, "GET", "/api/drop/"+created[0].ID+"/file", "")
	if rec.Header().Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("html served as %v", rec.Header())
	}
}

func TestDropRejectsCrossSitePosts(t *testing.T) {
	h, _ := setup(t)
	body, ct := form(t, map[string]string{"text": "spam"}, nil)
	if rec := post(h, "/api/drop", body, ct, "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", rec.Code)
	}
}

func TestShareTargetRedirectsToInbox(t *testing.T) {
	h, _ := setup(t)
	body, ct := form(t, map[string]string{"title": "A page", "text": "https://example.com/x"}, nil)
	rec := post(h, "/share", body, ct, "Sec-Fetch-Site", "same-origin")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/#/drop" {
		t.Fatalf("share: %d %v", rec.Code, rec.Header())
	}
	var list dropResponse
	json.Unmarshal(do(h, "GET", "/api/drop", "").Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Kind != "link" || list.Items[0].Title != "A page" {
		t.Fatalf("shared item: %+v", list.Items)
	}
}

func TestSendToAcceptingApp(t *testing.T) {
	var got struct{ field, name, body string }
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/foyer/widget":
			io.WriteString(w, `{"version":1,"accepts":{"url":"/api/books","types":[".epub"],"label":"Add to library"}}`)
		case "/api/books":
			f, hdr, err := r.FormFile("file")
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			b, _ := io.ReadAll(f)
			got.field, got.name, got.body = "file", hdr.Filename, string(b)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"message":"Added “Dune”","url":"/books/42"}`)
		}
	}))
	defer app.Close()

	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "foyer.yaml"))
	cfg := config.Default()
	cfg.Groups = []config.Group{{Name: "Books", Services: []config.Service{{
		Name: "Library", URL: "https://books.example.com",
		Widget: config.Widget{"type": "app", "url": app.URL + "/api/foyer/widget"},
	}}}}
	store.WriteInitial(cfg)
	h := New(store, monitor.New(store, nil, "/proc", "/sys"), nil, dir, nil).Handler()

	var targets []dropTarget
	json.Unmarshal(do(h, "GET", "/api/drop/targets", "").Body.Bytes(), &targets)
	if len(targets) != 1 || targets[0].Label != "Add to library" || targets[0].Service != "library" {
		t.Fatalf("targets: %+v", targets)
	}

	body, ct := form(t, nil, map[string]string{"Dune.epub": "epub bytes", "notes.txt": "x"})
	var created []drop.Item
	json.Unmarshal(post(h, "/api/drop", body, ct).Body.Bytes(), &created)
	byName := map[string]string{}
	for _, it := range created {
		byName[it.File.Name] = it.ID
	}
	send := func(id string) *httptest.ResponseRecorder {
		return post(h, "/api/drop/"+id+"/send", strings.NewReader(`{"service":"library"}`), "application/json")
	}
	rec := send(byName["Dune.epub"])
	var answer map[string]string
	json.Unmarshal(rec.Body.Bytes(), &answer)
	if rec.Code != 200 || answer["message"] != "Added “Dune”" || answer["url"] != "https://books.example.com/books/42" {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if got.name != "Dune.epub" || got.body != "epub bytes" {
		t.Fatalf("app received %+v", got)
	}
	if rec := send(byName["notes.txt"]); rec.Code != http.StatusBadRequest {
		t.Fatalf("sent a file the app doesn't take: %d", rec.Code)
	}
}

func TestManifest(t *testing.T) {
	h, _ := setup(t)
	rec := do(h, "GET", "/manifest.webmanifest", "")
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "Foyer" || m["share_target"] == nil {
		t.Fatalf("manifest: %v", m)
	}
}
