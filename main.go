package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"orcc/config"
	"orcc/models"
	"orcc/runner"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "help", "-h", "--help":
		printHelp()
	case "claude":
		runClaude(os.Args[2:])
	case "models":
		listModels(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`orcc %s — OpenRouter Claude Code launcher

Usage:
  orcc claude [args...]   Start Claude Code via OpenRouter (all args passed to claude)
  orcc models             List available OpenRouter models (pipes to fzf if installed)
  orcc help               Show this help

Config: ~/.config/orcc/config.yaml
`, version)
}

func listModels(_ []string) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting config path: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	if cfg.APIKey == "" {
		fmt.Fprintf(os.Stderr, "no api_key set in %s\n", cfgPath)
		os.Exit(1)
	}

	ids, err := models.FetchIDs("https://openrouter.ai/api/v1/models", cfg.APIKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching models: %v\n", err)
		os.Exit(1)
	}

	list := strings.Join(ids, "\n")

	fzf, err := exec.LookPath("fzf")
	if err != nil {
		fmt.Println(list)
		return
	}

	cmd := exec.Command(fzf)
	cmd.Stdin = strings.NewReader(list)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func runClaude(args []string) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting config path: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" {
		fmt.Fprintf(os.Stderr, "no api_key set in %s\nGet your key at https://openrouter.ai/keys\n", cfgPath)
		os.Exit(1)
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "claude not found in PATH: %v\n", err)
		os.Exit(1)
	}

	argv := runner.BuildArgs(cfg.DefaultModel, args)
	env := runner.BuildEnv(cfg)

	if err := syscall.Exec(claudeBin, argv, env); err != nil {
		fmt.Fprintf(os.Stderr, "exec claude: %v\n", err)
		os.Exit(1)
	}
}
