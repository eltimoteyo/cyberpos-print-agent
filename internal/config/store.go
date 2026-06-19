package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type PrinterConfig struct {
	PrinterName      string `json:"printerName"`
	ConnectionType   string `json:"connectionType"`
	PaperWidth       string `json:"paperWidth"`
	AutoCut          bool   `json:"autoCut"`
	OpenDrawerOnSale bool   `json:"openDrawerOnSale"`
}

func (c PrinterConfig) Validate() error {
	if strings.TrimSpace(c.PrinterName) == "" {
		return errors.New("printerName is required")
	}
	if strings.TrimSpace(c.ConnectionType) == "" {
		return errors.New("connectionType is required")
	}
	if strings.TrimSpace(c.PaperWidth) == "" {
		return errors.New("paperWidth is required")
	}
	return nil
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	return NewStoreWithDir("")
}

// NewStoreWithDir creates a store using the provided directory. If dir is empty,
// it falls back to the user config dir for backwards compatibility, unless the
// process is running as a Windows service in which case %PROGRAMDATA% is preferred.
func NewStoreWithDir(dir string) (*Store, error) {
	if dir == "" {
		dir = defaultDataDir()
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	return &Store{path: filepath.Join(dir, "config.json")}, nil
}

func defaultDataDir() string {
	if d := os.Getenv("PRINT_AGENT_DATA_DIR"); d != "" {
		return d
	}
	if pd := os.Getenv("PROGRAMDATA"); pd != "" {
		return filepath.Join(pd, "CyberERP", "PrintAgent")
	}
	baseDir, err := os.UserConfigDir()
	if err != nil {
		baseDir = "."
	}
	return filepath.Join(baseDir, "cybererp", "print-agent")
}

func (s *Store) Save(cfg PrinterConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}

func (s *Store) Load() (PrinterConfig, bool, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PrinterConfig{}, false, nil
		}
		return PrinterConfig{}, false, err
	}

	var cfg PrinterConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return PrinterConfig{}, false, err
	}

	return cfg, true, nil
}
