package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"orcc/config"
)

func TestLoadCreatesDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultModel == "" {
		t.Error("DefaultModel should have default value")
	}
	if cfg.Models.Opus == "" {
		t.Error("Models.Opus should have default value")
	}
	if cfg.Models.Sonnet == "" {
		t.Error("Models.Sonnet should have default value")
	}
	if cfg.Models.Haiku == "" {
		t.Error("Models.Haiku should have default value")
	}
	if cfg.Models.Subagent == "" {
		t.Error("Models.Subagent should have default value")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file should have been created on disk")
	}
}

func TestLoadParsesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `api_key: "sk-test-123"
default_model: "anthropic/claude-opus-4.7"
models:
  opus: "anthropic/claude-opus-4.7"
  sonnet: "anthropic/claude-sonnet-4.6"
  haiku: "anthropic/claude-haiku-4.5"
  subagent: "anthropic/claude-opus-4.7"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-test-123")
	}
	if cfg.DefaultModel != "anthropic/claude-opus-4.7" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "anthropic/claude-opus-4.7")
	}
	if cfg.Models.Sonnet != "anthropic/claude-sonnet-4.6" {
		t.Errorf("Models.Sonnet = %q", cfg.Models.Sonnet)
	}
}

func TestLoadErrorsOnBadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(":\t:invalid yaml{{"), 0600)

	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestConfigDirDefault(t *testing.T) {
	home, _ := os.UserHomeDir()
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error: %v", err)
	}
	want := filepath.Join(home, ".config", "orcc", "config.yaml")
	if got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
