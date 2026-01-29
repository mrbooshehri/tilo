package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Rule struct {
	Pattern string `yaml:"pattern"`
	Color   string `yaml:"color"`
	Style   string `yaml:"style"`
}

type Config struct {
	Colors         map[string]string `yaml:"colors"`
	DisableBuiltin []string          `yaml:"disable_builtin"`
	CustomRules    []Rule            `yaml:"custom_rules"`
	StatusBar      string            `yaml:"status_bar"`
}

func Load(path string) (Config, error) {
	if path == "" {
		found, err := findDefaultConfig()
		if err != nil {
			return Config{}, err
		}
		path = found
	}
	if path == "" {
		return Config{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	normalize(&cfg)
	return cfg, nil
}

func normalize(cfg *Config) {
	if cfg.Colors == nil {
		cfg.Colors = map[string]string{}
	}
	for k, v := range cfg.Colors {
		cfg.Colors[strings.ToLower(k)] = strings.ToLower(v)
	}
	for i := range cfg.DisableBuiltin {
		cfg.DisableBuiltin[i] = strings.ToLower(cfg.DisableBuiltin[i])
	}
	for i := range cfg.CustomRules {
		cfg.CustomRules[i].Color = strings.ToLower(cfg.CustomRules[i].Color)
		cfg.CustomRules[i].Style = strings.ToLower(cfg.CustomRules[i].Style)
	}
	cfg.StatusBar = strings.ToLower(strings.TrimSpace(cfg.StatusBar))
}

func findDefaultConfig() (string, error) {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg != "" {
		candidate := filepath.Join(xdg, "tilo", "config.yaml")
		if exists(candidate) {
			return candidate, nil
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	paths := []string{
		filepath.Join(home, ".config", "tilo", "config.yaml"),
		filepath.Join(home, ".tilo.yaml"),
	}
	for _, p := range paths {
		if exists(p) {
			return p, nil
		}
	}
	return "", nil
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

var ErrNoInput = errors.New("no input provided")
