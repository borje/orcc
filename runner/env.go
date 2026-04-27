package runner

import (
	"fmt"
	"os"
	"strings"

	"orcc/config"
)

var overrideKeys = map[string]bool{
	"ANTHROPIC_BASE_URL":             true,
	"ANTHROPIC_AUTH_TOKEN":           true,
	"ANTHROPIC_API_KEY":              true,
	"ANTHROPIC_DEFAULT_OPUS_MODEL":   true,
	"ANTHROPIC_DEFAULT_SONNET_MODEL": true,
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":  true,
	"CLAUDE_CODE_SUBAGENT_MODEL":     true,
}

func BuildEnv(cfg *config.Config) []string {
	base := os.Environ()
	filtered := make([]string, 0, len(base)+len(overrideKeys))
	for _, e := range base {
		k := strings.SplitN(e, "=", 2)[0]
		if !overrideKeys[k] {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered,
		"ANTHROPIC_BASE_URL=https://openrouter.ai/api",
		fmt.Sprintf("ANTHROPIC_AUTH_TOKEN=%s", cfg.APIKey),
		"ANTHROPIC_API_KEY=",
		fmt.Sprintf("ANTHROPIC_DEFAULT_OPUS_MODEL=%s", cfg.Models.Opus),
		fmt.Sprintf("ANTHROPIC_DEFAULT_SONNET_MODEL=%s", cfg.Models.Sonnet),
		fmt.Sprintf("ANTHROPIC_DEFAULT_HAIKU_MODEL=%s", cfg.Models.Haiku),
		fmt.Sprintf("CLAUDE_CODE_SUBAGENT_MODEL=%s", cfg.Models.Subagent),
	)
	return filtered
}

// BuildArgs builds the claude argv. If extra already contains --model, the
// default is omitted so the user's value takes precedence.
func BuildArgs(defaultModel string, extra []string) []string {
	for _, a := range extra {
		if a == "--model" {
			return append([]string{"claude"}, extra...)
		}
	}
	args := []string{"claude", "--model", defaultModel}
	return append(args, extra...)
}
