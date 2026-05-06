package runner_test

import (
	"reflect"
	"strings"
	"testing"

	"orcc/config"
	"orcc/runner"
)

func TestBuildEnv(t *testing.T) {
	cfg := &config.Config{
		APIKey:       "sk-test",
		DefaultModel: "anthropic/claude-sonnet-4.6",
		Models: config.Models{
			Opus:    "anthropic/claude-opus-4.7",
			Sonnet:  "anthropic/claude-sonnet-4.6",
			Haiku:   "anthropic/claude-haiku-4.5",
			Subagent: "anthropic/claude-opus-4.7",
		},
	}

	env := runner.BuildEnv(cfg, "")

	got := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			got[parts[0]] = parts[1]
		} else {
			got[parts[0]] = ""
		}
	}

	want := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://openrouter.ai/api",
		"ANTHROPIC_AUTH_TOKEN":           "sk-test",
		"ANTHROPIC_API_KEY":              "",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "anthropic/claude-opus-4.7",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "anthropic/claude-sonnet-4.6",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "anthropic/claude-haiku-4.5",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "anthropic/claude-opus-4.7",
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestBuildEnvOverridesExisting(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "should-be-cleared")
	t.Setenv("ANTHROPIC_BASE_URL", "should-be-overridden")

	cfg := &config.Config{
		APIKey: "new-key",
		Models: config.Models{},
	}

	env := runner.BuildEnv(cfg, "")

	counts := make(map[string]int)
	for _, e := range env {
		k := strings.SplitN(e, "=", 2)[0]
		counts[k]++
	}

	for k, n := range counts {
		if n > 1 {
			t.Errorf("duplicate env var %s (count=%d)", k, n)
		}
	}
}

func TestBuildEnvLogDir(t *testing.T) {
	cfg := &config.Config{APIKey: "sk-test", Models: config.Models{}}

	t.Run("log dir set", func(t *testing.T) {
		env := runner.BuildEnv(cfg, "/tmp/logs")
		got := make(map[string]string)
		for _, e := range env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				got[parts[0]] = parts[1]
			}
		}
		if got["OTEL_LOG_RAW_API_BODIES"] != "file:/tmp/logs" {
			t.Errorf("OTEL_LOG_RAW_API_BODIES = %q, want %q", got["OTEL_LOG_RAW_API_BODIES"], "file:/tmp/logs")
		}
	})

	t.Run("log dir empty", func(t *testing.T) {
		env := runner.BuildEnv(cfg, "")
		for _, e := range env {
			if strings.HasPrefix(e, "OTEL_LOG_RAW_API_BODIES=") {
				t.Errorf("unexpected OTEL_LOG_RAW_API_BODIES in env: %s", e)
			}
		}
	})

	t.Run("strips pre-existing OTEL var", func(t *testing.T) {
		t.Setenv("OTEL_LOG_RAW_API_BODIES", "file:/old/path")
		env := runner.BuildEnv(cfg, "")
		for _, e := range env {
			if strings.HasPrefix(e, "OTEL_LOG_RAW_API_BODIES=") {
				t.Errorf("pre-existing OTEL_LOG_RAW_API_BODIES not stripped: %s", e)
			}
		}
	})
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name         string
		defaultModel string
		extra        []string
		want         []string
	}{
		{
			name:         "no extra args",
			defaultModel: "anthropic/claude-sonnet-4.6",
			extra:        nil,
			want:         []string{"claude", "--model", "anthropic/claude-sonnet-4.6"},
		},
		{
			name:         "with extra args",
			defaultModel: "anthropic/claude-sonnet-4.6",
			extra:        []string{"--print", "hello"},
			want:         []string{"claude", "--model", "anthropic/claude-sonnet-4.6", "--print", "hello"},
		},
		{
			name:         "user supplies --model overrides default",
			defaultModel: "anthropic/claude-sonnet-4.6",
			extra:        []string{"--model", "anthropic/claude-opus-4.7"},
			want:         []string{"claude", "--model", "anthropic/claude-opus-4.7"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runner.BuildArgs(tc.defaultModel, tc.extra)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}
