package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configFileName = "config.json"
)

// Config holds the .flow/config.json settings.
type Config struct {
	Backend     string `json:"backend"`      // "terraform" or "pulumi"
	Registry    string `json:"registry"`      // registry base URL
	ModuleCache string `json:"module_cache"`  // path to module cache dir
}

// DefaultConfig returns the default config.
func DefaultConfig() *Config {
	return &Config{
		Backend:     "terraform",
		Registry:    "https://registry.terraform.io/v1/modules",
		ModuleCache: filepath.Join(flowDir, "modules"),
	}
}

// LoadConfig loads config from .flow/config.json, returning defaults if not found.
func LoadConfig() (*Config, error) {
	path := filepath.Join(flowDir, configFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults for empty fields
	if cfg.Backend == "" {
		cfg.Backend = "terraform"
	}
	if cfg.Registry == "" {
		cfg.Registry = "https://registry.terraform.io/v1/modules"
	}
	if cfg.ModuleCache == "" {
		cfg.ModuleCache = filepath.Join(flowDir, "modules")
	}

	return cfg, nil
}

// SaveConfig writes config to .flow/config.json.
func SaveConfig(cfg *Config) error {
	if err := os.MkdirAll(flowDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", flowDir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(flowDir, configFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}
