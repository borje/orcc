package runner

import (
	"fmt"
	"os"
	"strings"

	"orcc/config"
)

var overrideKeys = map[string]bool{
	"ANTHROPIC_BASE_URL":                   true,
	"ANTHROPIC_AUTH_TOKEN":                 true,
	"ANTHROPIC_API_KEY":                    true,
	"ANTHROPIC_DEFAULT_OPUS_MODEL":         true,
	"ANTHROPIC_DEFAULT_SONNET_MODEL":       true,
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":        true,
	"CLAUDE_CODE_SUBAGENT_MODEL":           true,
	"CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION": true,
	"ENABLE_CLAUDEAI_MCP_SERVERS":          true,
	"OTEL_LOG_RAW_API_BODIES":              true,
}

func BuildEnv(cfg *config.Config, logDir string) []string {
	base := os.Environ()
	filtered := make([]string, 0, len(base)+len(overrideKeys))
	for _, e := range base {
		k := strings.SplitN(e, "=", 2)[0]
		if !overrideKeys[k] {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered,
		fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:%d", cfg.Port),
		fmt.Sprintf("ANTHROPIC_AUTH_TOKEN=%s", cfg.APIKey),
		"ANTHROPIC_API_KEY=",
		fmt.Sprintf("ANTHROPIC_DEFAULT_OPUS_MODEL=%s", cfg.Models.Opus),
		fmt.Sprintf("ANTHROPIC_DEFAULT_SONNET_MODEL=%s", cfg.Models.Sonnet),
		fmt.Sprintf("ANTHROPIC_DEFAULT_HAIKU_MODEL=%s", cfg.Models.Haiku),
		fmt.Sprintf("CLAUDE_CODE_SUBAGENT_MODEL=%s", cfg.Models.Subagent),
		// Claude Code's "suggest next prompt" feature re-sends the whole
		// conversation each turn just to predict your next input — pure waste on
		// a metered OpenRouter model.
		"CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION=false",
		// claude.ai org connectors can't work under API-key (OpenRouter) auth;
		// disabling the fetch avoids the connector warning in the child process.
		"ENABLE_CLAUDEAI_MCP_SERVERS=false",
	)
	if logDir != "" {
		filtered = append(filtered, fmt.Sprintf("OTEL_LOG_RAW_API_BODIES=file:%s", logDir))
	}
	return filtered
}

// BuildArgs builds the claude argv. If extra already contains --model, the
// default is omitted so the user's value takes precedence.
func BuildArgs(defaultModel string, extra []string) []string {
	for _, a := range extra {
		if a == "--model" || strings.HasPrefix(a, "--model=") {
			return append([]string{"claude"}, extra...)
		}
	}
	args := []string{"claude", "--model", defaultModel}
	return append(args, extra...)
}
