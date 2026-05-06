# orcc

Run Claude Code via [OpenRouter](https://openrouter.ai) instead of the Anthropic API.

## Why

OpenRouter gives you access to Claude models through a single API key, with unified billing across providers.

## Install

```bash
go install github.com/borje/orcc@latest
```

Or build from source:

```bash
git clone https://github.com/borje/orcc
cd orcc
go install .
```

## Setup

1. Get an API key at <https://openrouter.ai/keys>
2. Add it to `~/.config/orcc/config.yaml` (created automatically on first run):

```yaml
api_key: "sk-or-..."
```

## Usage

```bash
orcc claude                  # start Claude Code
orcc claude --print "hello"  # pass args to claude
orcc models                  # list available models (pipes to fzf if installed)
```

All arguments after `claude` are passed through to the `claude` CLI unchanged.

## Configuration

`~/.config/orcc/config.yaml`:

```yaml
api_key: "sk-or-..."

# Model used when you don't pass --model
default_model: "anthropic/claude-sonnet-4.6"

# Models used internally by Claude Code for different roles
models:
  opus: "anthropic/claude-opus-4.7"
  sonnet: "anthropic/claude-sonnet-4.6"
  haiku: "anthropic/claude-haiku-4.5"
  subagent: "anthropic/claude-opus-4.7"
```

You can swap in any model available on OpenRouter — not just Anthropic ones.

## How it works

`orcc claude` replaces itself (via `exec`) with the `claude` process after setting:

- `ANTHROPIC_BASE_URL=https://openrouter.ai/api`
- `ANTHROPIC_AUTH_TOKEN=<your key>`
- model env vars for each Claude Code role

Claude Code never knows it's talking to OpenRouter.

## Requirements

- Go 1.21+
- [Claude Code](https://claude.ai/code) installed (`claude` in PATH)
