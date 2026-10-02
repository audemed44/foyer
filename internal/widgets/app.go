package widgets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/audemed44/foyer/internal/config"
)

// The Foyer widget format lets an app describe its own card. Version 1:
//
//	{
//	  "version": 1,
//	  "stats":    [{"label", "value", "unit"?, "caption"?, "tone"?}],
//	  "progress": [{"label", "value", "max", "caption"?}],
//	  "items_title": "…", "items_layout": "covers" | "list",
//	  "items":    [{"title", "subtitle"?, "image"?, "url"?, "progress"?, "caption"?,
//	                "action"?: {"label", "url", "confirm"?}}],
//	  "accepts":  {"url", "types", "label"?, "field"?}
//	}
//
// Relative image paths are served by the app and fetched through Foyer
// (ProxyImage); relative item URLs resolve against the service's link.
// Actions are POSTed by Foyer to the app's own address (RunAction).

// WellKnownPath is where apps serve their widget; discovery probes it.
const WellKnownPath = "/api/foyer/widget"

type AppStat struct {
	Label   string `json:"label"`
	Value   string `json:"value"`
	Unit    string `json:"unit,omitempty"`
	Caption string `json:"caption,omitempty"`
	Tone    string `json:"tone,omitempty"` // good | warn | bad | accent
}

type AppProgress struct {
	Label   string  `json:"label"`
	Value   float64 `json:"value"`
	Max     float64 `json:"max"`
	Caption string  `json:"caption,omitempty"`
}

type AppItem struct {
	Title    string     `json:"title"`
	Subtitle string     `json:"subtitle,omitempty"`
	Image    string     `json:"image,omitempty"`
	URL      string     `json:"url,omitempty"`
	Progress *float64   `json:"progress,omitempty"` // 0-100
	Caption  string     `json:"caption,omitempty"`
	Action   *AppAction `json:"action,omitempty"`
}

// AppAction is a button on an item, e.g. Hoist's "Deploy" on a stack.
type AppAction struct {
	Label   string `json:"label"`
	URL     string `json:"url"`               // a path on the app, POSTed to
	Confirm string `json:"confirm,omitempty"` // asked before running it
}

// AppAccepts declares that the app takes uploads of some file types.
type AppAccepts struct {
	URL   string   `json:"url"`
	Types []string `json:"types"` // extensions (".epub") or MIME types
	Label string   `json:"label,omitempty"`
	Field string   `json:"field,omitempty"`
}

type AppWidget struct {
	Version     int           `json:"version"`
	Stats       []AppStat     `json:"stats"`
	Progress    []AppProgress `json:"progress"`
	ItemsTitle  string        `json:"items_title,omitempty"`
	ItemsLayout string        `json:"items_layout"`
	Items       []AppItem     `json:"items"`
	Accepts     *AppAccepts   `json:"accepts,omitempty"`
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// validTone and limits keep a misbehaving app from breaking the card.
var validTone = map[string]bool{"good": true, "warn": true, "bad": true, "accent": true}

func (w *AppWidget) sanitize() error {
	if w.Version != 1 {
		return fmt.Errorf("unsupported widget version %d", w.Version)
	}
	w.Stats = w.Stats[:min(len(w.Stats), 6)]
	for i := range w.Stats {
		s := &w.Stats[i]
		s.Label, s.Value, s.Unit, s.Caption = clip(s.Label, 40), clip(s.Value, 16), clip(s.Unit, 16), clip(s.Caption, 60)
		if !validTone[s.Tone] {
			s.Tone = ""
		}
	}
	w.Progress = w.Progress[:min(len(w.Progress), 4)]
	for i := range w.Progress {
		p := &w.Progress[i]
		p.Label, p.Caption = clip(p.Label, 40), clip(p.Caption, 60)
	}
	w.Items = w.Items[:min(len(w.Items), 12)]
	for i := range w.Items {
		it := &w.Items[i]
		it.Title, it.Subtitle, it.Caption = clip(it.Title, 120), clip(it.Subtitle, 120), clip(it.Caption, 40)
		if it.Progress != nil {
			v := max(0, min(100, *it.Progress))
			it.Progress = &v
		}
		if !safeLink(it.URL) {
			it.URL = ""
		}
		if !relativePath(it.Image) && !strings.HasPrefix(it.Image, "https://") {
			it.Image = ""
		}
		if a := it.Action; a != nil {
			a.Label, a.Confirm = clip(a.Label, 24), clip(a.Confirm, 200)
			if a.Label == "" {
				a.Label = "Run"
			}
			if !relativePath(a.URL) {
				it.Action = nil
			}
		}
	}
	if w.ItemsLayout != "covers" {
		w.ItemsLayout = "list"
	}
	w.ItemsTitle = clip(w.ItemsTitle, 40)
	if a := w.Accepts; a != nil {
		types := []string{}
		for _, t := range a.Types[:min(len(a.Types), 20)] {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				types = append(types, clip(t, 80))
			}
		}
		a.Types, a.Label, a.Field = types, clip(a.Label, 40), clip(a.Field, 40)
		if a.Field == "" {
			a.Field = "file"
		}
		if !relativePath(a.URL) || len(types) == 0 {
			w.Accepts = nil
		}
	}
	if w.Stats == nil {
		w.Stats = []AppStat{}
	}
	if w.Progress == nil {
		w.Progress = []AppProgress{}
	}
	if w.Items == nil {
		w.Items = []AppItem{}
	}
	return nil
}

// relativePath is an absolute path on the same origin ("/x"), not "//host".
func relativePath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.Contains(p, "\\")
}

func safeLink(u string) bool {
	return u == "" || relativePath(u) || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// app fetches a widget served by the app itself.
// Settings: url, key (optional bearer token).
func app(ctx context.Context, w config.Widget) (any, error) {
	if err := required(w, "url"); err != nil {
		return nil, err
	}
	var headers []string
	if key := w.String("key"); key != "" {
		headers = []string{"Authorization", "Bearer " + key}
	}
	var out AppWidget
	if err := getJSON(ctx, w.String("url"), &out, headers...); err != nil {
		return nil, err
	}
	if err := out.sanitize(); err != nil {
		return nil, err
	}
	return out, nil
}

var errNotImage = errors.New("not an image")

// ProxyImage streams an image the app serves at path (from the widget's
// data) to the browser, so the app's internal address stays private. Only
// same-origin paths are allowed.
func ProxyImage(ctx context.Context, w config.Widget, path string, dst http.ResponseWriter) error {
	if !IsApp(w) || !relativePath(path) {
		return fmt.Errorf("invalid image path")
	}
	base, err := url.Parse(config.ExpandEnv(w.String("url")))
	if err != nil || base.Host == "" {
		return fmt.Errorf("invalid widget url")
	}
	target, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid image path")
	}
	src := base.ResolveReference(target)
	src.Scheme, src.Host, src.User = base.Scheme, base.Host, nil

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.String(), nil)
	if err != nil {
		return err
	}
	if key := config.ExpandEnv(w.String("key")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s", src.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered HTTP %d", src.Host, resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		// SVG can carry script; the other formats can't.
		return errNotImage
	}
	dst.Header().Set("Content-Type", ct)
	dst.Header().Set("Cache-Control", "public, max-age=3600")
	dst.Header().Set("Content-Security-Policy", "default-src 'none'")
	_, err = io.Copy(dst, io.LimitReader(resp.Body, 10<<20))
	return err
}

// Probe reports whether an app at base (scheme://host:port) serves a Foyer
// widget, returning the widget URL when it does.
func Probe(ctx context.Context, base string) (string, bool) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", false
	}
	u.Path, u.RawQuery, u.Fragment = WellKnownPath, "", ""
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var out struct {
		Version int `json:"version"`
	}
	if err := getJSON(ctx, u.String(), &out); err != nil || out.Version != 1 {
		return "", false
	}
	return u.String(), true
}

// Accepts reports whether an app's accepts rule covers a file.
func (a *AppAccepts) Accepts(name, contentType string) bool {
	if a == nil {
		return false
	}
	ext := strings.ToLower(path.Ext(name))
	contentType = strings.ToLower(contentType)
	for _, t := range a.Types {
		if (strings.HasPrefix(t, ".") && t == ext) || t == contentType || t == "*/*" ||
			(strings.HasSuffix(t, "/*") && strings.HasPrefix(contentType, strings.TrimSuffix(t, "*"))) {
			return true
		}
	}
	return false
}

// HasAction reports whether an item of the widget offers an action at url,
// so Foyer only ever POSTs to actions the app itself listed.
func (w AppWidget) HasAction(url string) bool {
	for _, it := range w.Items {
		if it.Action != nil && it.Action.URL == url {
			return true
		}
	}
	return false
}

// appURL resolves a path against the app's own address (from the widget
// URL), never a host the widget data names.
func appURL(w config.Widget, p string) (string, error) {
	base, err := url.Parse(config.ExpandEnv(w.String("url")))
	if err != nil || base.Host == "" || !relativePath(p) {
		return "", fmt.Errorf("invalid address")
	}
	ref, err := url.Parse(p)
	if err != nil {
		return "", fmt.Errorf("invalid address")
	}
	dst := base.ResolveReference(ref)
	dst.Scheme, dst.Host, dst.User = base.Scheme, base.Host, nil
	return dst.String(), nil
}

// ActionResult is what an app answers an action, or its status, with. All
// fields are optional. StatusURL (a path on the app) is polled while State
// is "running"; State is "running", "done" or "failed".
type ActionResult struct {
	Message   string `json:"message"`
	URL       string `json:"url,omitempty"`
	StatusURL string `json:"status_url,omitempty"`
	State     string `json:"state"`
}

func (r *ActionResult) sanitize() {
	r.Message = clip(r.Message, 200)
	if !safeLink(r.URL) {
		r.URL = ""
	}
	if !relativePath(r.StatusURL) {
		r.StatusURL = ""
	}
	switch r.State {
	case "running", "done", "failed":
	default:
		// No state: running while there's something to poll, else done.
		r.State = "done"
		if r.StatusURL != "" {
			r.State = "running"
		}
	}
}

func actionCall(ctx context.Context, w config.Widget, method, p string) (ActionResult, error) {
	dst, err := appURL(w, p)
	if err != nil {
		return ActionResult{}, err
	}
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, dst, body)
	if err != nil {
		return ActionResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key := config.ExpandEnv(w.String("key")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return ActionResult{}, fmt.Errorf("could not reach %s", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ActionResult{}, fmt.Errorf("%s", errorMessage(resp))
	}
	var res ActionResult
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&res)
	res.sanitize()
	return res, nil
}

// RunAction POSTs to an action the widget lists.
func RunAction(ctx context.Context, w config.Widget, p string) (ActionResult, error) {
	return actionCall(ctx, w, http.MethodPost, p)
}

// ActionStatus fetches the status of a running action.
func ActionStatus(ctx context.Context, w config.Widget, p string) (ActionResult, error) {
	return actionCall(ctx, w, http.MethodGet, p)
}

// Sent is what an app may answer an upload with: a line to show, and a
// link to what it made (a path relative to the app's public address, or a
// full URL). Both are optional.
type Sent struct {
	Message string `json:"message"`
	URL     string `json:"url"`
}

// SendFile uploads a file to an app, as described by its widget's accepts
// rule. The request goes to the app's own address (from the widget URL),
// never to one the widget data names.
func SendFile(ctx context.Context, w config.Widget, a *AppAccepts, name, contentType string, body io.Reader) (Sent, error) {
	base, err := url.Parse(config.ExpandEnv(w.String("url")))
	if err != nil || base.Host == "" || !relativePath(a.URL) {
		return Sent{}, fmt.Errorf("invalid upload address")
	}
	target, err := url.Parse(a.URL)
	if err != nil {
		return Sent{}, fmt.Errorf("invalid upload address")
	}
	dst := base.ResolveReference(target)
	dst.Scheme, dst.Host, dst.User = base.Scheme, base.Host, nil

	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(a.Field), quoteEscaper.Replace(name)))
		header.Set("Content-Type", contentType)
		part, err := form.CreatePart(header)
		if err == nil {
			_, err = io.Copy(part, body)
		}
		if err == nil {
			err = form.Close()
		}
		pw.CloseWithError(err)
	}()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.String(), pr)
	if err != nil {
		pr.Close()
		return Sent{}, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if key := config.ExpandEnv(w.String("key")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		pr.Close()
		return Sent{}, fmt.Errorf("could not reach %s", dst.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Sent{}, fmt.Errorf("%s", errorMessage(resp))
	}
	var sent Sent
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&sent)
	sent.Message = clip(sent.Message, 200)
	if !safeLink(sent.URL) {
		sent.URL = ""
	}
	return sent, nil
}

var (
	uploadClient = &http.Client{} // bounded by the request context
	quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "")
)

// errorMessage pulls a readable message out of an error response.
func errorMessage(resp *http.Response) string {
	var body struct {
		Detail  any    `json:"detail"` // FastAPI
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(raw, &body) == nil {
		if s, ok := body.Detail.(string); ok && s != "" {
			return clip(s, 200)
		}
		for _, s := range []string{body.Error, body.Message} {
			if s != "" {
				return clip(s, 200)
			}
		}
	}
	return fmt.Sprintf("the app answered HTTP %d", resp.StatusCode)
}
