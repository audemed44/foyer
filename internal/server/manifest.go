package server

import (
	"encoding/json"
	"net/http"
)

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type shortcut struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// manifest describes the installable app. It's built per request so the
// app's name follows the dashboard title and its colours the theme.
func (s *Server) manifest(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.Config()
	bg := "#000000"
	if cfg.Theme.Mode == "light" {
		bg = "#f6f6f4"
	}
	short := cfg.Title
	if r := []rune(short); len(r) > 12 {
		short = string(r[:12])
	}
	m := map[string]any{
		"name":             cfg.Title,
		"short_name":       short,
		"id":               "/",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"background_color": bg,
		"theme_color":      bg,
		"icons": []manifestIcon{
			{Src: "/icon-192.png", Sizes: "192x192", Type: "image/png"},
			{Src: "/icon-512.png", Sizes: "512x512", Type: "image/png"},
			{Src: "/icon-maskable-512.png", Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
		},
		"shortcuts": []shortcut{
			{Name: "Drop", URL: "/#/drop"},
			{Name: "Containers", URL: "/#/containers"},
		},
		// Sharing a link, text or files to the installed app puts them in Drop.
		"share_target": map[string]any{
			"action":  "/share",
			"method":  "POST",
			"enctype": "multipart/form-data",
			"params": map[string]any{
				"title": "title", "text": "text", "url": "url",
				"files": []map[string]any{{"name": "files", "accept": []string{"*/*"}}},
			},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(m)
}
