package drop

import (
	"context"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	titleClient  = &http.Client{Timeout: 6 * time.Second}
	titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	spaces       = regexp.MustCompile(`\s+`)
)

// FetchTitle reads a page's <title>, for links shared without one.
func FetchTitle(ctx context.Context, url string) string {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Foyer)")
	req.Header.Set("Accept", "text/html")
	resp, err := titleClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		return ""
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	m := titlePattern.FindSubmatch(head)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(string(m[1])), " "))
}
