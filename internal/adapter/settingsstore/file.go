// Package settingsstore keeps the admin's AI choice in a small JSON file so it
// survives restarts. The file holds the API key: it is created 0600 and lives
// on a volume only this service mounts.
package settingsstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"goodfood/assistant-service/internal/application"
)

type File struct{ path string }

func NewFile(path string) *File { return &File{path: path} }

type record struct {
	Provider       string `json:"provider"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (f *File) Load() (*application.AISettings, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r record
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &application.AISettings{Provider: r.Provider, BaseURL: r.BaseURL, APIKey: r.APIKey, Model: r.Model, TimeoutSeconds: r.TimeoutSeconds}, nil
}

// Save writes atomically so a crash never leaves a half-written file.
func (f *File) Save(s application.AISettings) error {
	raw, err := json.Marshal(record{s.Provider, s.BaseURL, s.APIKey, s.Model, s.TimeoutSeconds})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *File) Clear() error {
	if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
