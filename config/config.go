package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Models struct {
	Opus    string `yaml:"opus"`
	Sonnet  string `yaml:"sonnet"`
	Haiku   string `yaml:"haiku"`
	Subagent string `yaml:"subagent"`
}

type Config struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	Models       Models `yaml:"models"`
}

var defaults = Config{
	APIKey:       "",
	DefaultModel: "anthropic/claude-sonnet-4.6",
	Models: Models{
		Opus:    "anthropic/claude-opus-4.7",
		Sonnet:  "anthropic/claude-sonnet-4.6",
		Haiku:   "anthropic/claude-haiku-4.5",
		Subagent: "anthropic/claude-opus-4.7",
	},
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "orcc", "config.yaml"), nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := createDefault(path); err != nil {
			return nil, fmt.Errorf("create default config: %w", err)
		}
		cfg := defaults
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func createDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := `# orcc configuration
# Get your API key at https://openrouter.ai/keys
api_key: ""

default_model: "anthropic/claude-sonnet-4.6"

models:
  opus: "anthropic/claude-opus-4.7"
  sonnet: "anthropic/claude-sonnet-4.6"
  haiku: "anthropic/claude-haiku-4.5"
  subagent: "anthropic/claude-opus-4.7"
`
	return os.WriteFile(path, []byte(content), 0600)
}
