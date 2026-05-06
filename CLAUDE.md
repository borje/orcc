# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build ./...                          # build
go test ./...                           # all tests
go test ./config/ -run TestName         # single test
go run . claude --help                  # run without installing
go install .                            # install orcc to $GOPATH/bin
```

## Architecture

`orcc` is a thin launcher that makes Claude Code use OpenRouter instead of Anthropic's API. `orcc claude [args]` calls `syscall.Exec` to replace itself with the `claude` process, injecting env vars that redirect it:

```
ANTHROPIC_BASE_URL=https://openrouter.ai/api
ANTHROPIC_AUTH_TOKEN=<key>        # OpenRouter key
ANTHROPIC_API_KEY=                # cleared so Claude Code uses AUTH_TOKEN
ANTHROPIC_DEFAULT_OPUS_MODEL=...
ANTHROPIC_DEFAULT_SONNET_MODEL=...
ANTHROPIC_DEFAULT_HAIKU_MODEL=...
CLAUDE_CODE_SUBAGENT_MODEL=...
```

`runner.BuildEnv` strips any existing values for those keys from the parent env before appending the correct ones — prevents duplicates when the user already has Anthropic vars set.

`runner.BuildArgs` prepends `--model <default_model>` unless the user already passed `--model`.

Config lives at `~/.config/orcc/config.yaml` (auto-created with defaults on first run). `config.Load` returns defaults when the file is absent, and creates the file as a side-effect.

`models.FetchIDs` hits `GET /api/v1/models` with a Bearer token and returns model IDs. `orcc models` pipes that list to `fzf` when available.
