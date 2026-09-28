package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Store serves the config file, re-reading it whenever it changes on disk so
// hand edits apply without a restart.
type Store struct {
	path string

	mu      sync.Mutex
	current Config
	modTime time.Time
	err     error
}

func NewStore(path string) *Store {
	s := &Store{path: path, current: Default()}
	_ = s.current.Normalize()
	return s
}

func (s *Store) Path() string { return s.path }

// Get returns the current config and the last load error, if the file on
// disk is invalid (the previous good config keeps being served).
func (s *Store) Get() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Stat(s.path)
	if err == nil && !info.ModTime().Equal(s.modTime) {
		s.modTime = info.ModTime()
		s.load()
	}
	return s.current, s.err
}

// Config is Get without the error, for callers that only need the values.
func (s *Store) Config() Config {
	c, _ := s.Get()
	return c
}

func (s *Store) load() {
	cfg, err := Parse(s.path)
	if err != nil {
		s.err = err
		slog.Warn("invalid config, keeping the previous one", "err", err)
		return
	}
	s.current, s.err = cfg, nil
}

// Parse reads and validates a config file.
func Parse(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := cfg.Normalize(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return cfg, nil
}

// Save validates and writes an edited config, keeping a .bak of the old file.
func (s *Store) Save(cfg Config) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg.RestoreSecrets(s.current)
	if err := cfg.Normalize(); err != nil {
		return Config{}, err
	}
	if err := s.write(cfg); err != nil {
		return Config{}, err
	}
	info, err := os.Stat(s.path)
	if err == nil {
		s.modTime = info.ModTime()
	}
	s.current, s.err = cfg, nil
	return cfg, nil
}

// WriteInitial creates the config file; used on first start.
func (s *Store) WriteInitial(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := cfg.Normalize(); err != nil {
		return err
	}
	return s.write(cfg)
}

func (s *Store) write(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(s.path); err == nil {
		_ = os.WriteFile(s.path+".bak", old, 0o644)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Marshal renders a config as YAML. Ids come from names, so they're dropped.
func Marshal(cfg Config) ([]byte, error) {
	cfg = cfg.clone()
	for gi := range cfg.Groups {
		cfg.Groups[gi].ID = ""
		for si := range cfg.Groups[gi].Services {
			cfg.Groups[gi].Services[si].ID = ""
		}
	}
	var buf bytes.Buffer
	buf.WriteString("# Foyer config. Edit it here or from the dashboard's edit mode;\n")
	buf.WriteString("# changes to this file apply without a restart.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${VAR} references with environment values.
func ExpandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[2 : len(m)-1])
	})
}
