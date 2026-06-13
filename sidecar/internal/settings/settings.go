package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	SidecarEnrichmentEnabled bool `json:"sidecar_enrichment_enabled"`
}

func Load(dir string, defaultEnabled bool) (*Settings, error) {
	path := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Settings{SidecarEnrichmentEnabled: defaultEnabled}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}

func Save(dir string, s *Settings) error {
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
