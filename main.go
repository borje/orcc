package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"orcc/config"
	"orcc/daemon"
	"orcc/models"
	"orcc/proxy"
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
	case "start":
		startDaemon()
	case "stop":
		stopDaemon()
	case "restart":
		stopDaemon()
		startDaemon()
	case "status":
		printStatus()
	case "serve":
		serveProxy()
	case "claude":
		runClaude(os.Args[2:])
	case "models":
		listModels(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`orcc %s — OpenRouter Claude Code launcher

Usage:
  orcc start                              Start the proxy daemon
  orcc stop                               Stop the proxy daemon
  orcc restart                            Restart the proxy daemon
  orcc status                             Show proxy daemon status
  orcc claude [args...]                   Start Claude Code via OpenRouter (all args passed to claude)
  orcc claude --orcc-log-dir <dir> [args] Also log raw API requests/responses to <dir>
  orcc models [--sort price|name|none]     List available OpenRouter models (pipes to fzf if installed)
  orcc version                             Show version
  orcc help                                Show this help

Config: ~/.config/orcc/config.yaml
`, version)
}

func listModels(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ms, err := models.FetchModels("https://openrouter.ai/api/v1/models", cfg.APIKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching models: %v\n", err)
		os.Exit(1)
	}

	sortMode := "price"
	for i, a := range args {
		if a == "--sort" && i+1 < len(args) {
			sortMode = args[i+1]
		}
	}
	switch sortMode {
	case "price":
		models.SortByPrice(ms)
	case "name":
		models.SortByName(ms)
	case "none":
	default:
		fmt.Fprintf(os.Stderr, "unknown sort mode: %s (use price, name, or none)\n", sortMode)
		os.Exit(1)
	}

	lines := make([]string, len(ms))
	for i, m := range ms {
		lines[i] = models.FormatLine(m)
	}
	list := strings.Join(lines, "\n")

	fzf, err := exec.LookPath("fzf")
	if err != nil {
		fmt.Println(list)
		return
	}

	cmd := exec.Command(fzf, "+s")
	cmd.Stdin = strings.NewReader(list)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func startDaemon() {
	_, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	pidPath, err := daemon.PidPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if pid, _ := daemon.ReadPid(pidPath); pid != 0 && daemon.IsRunning(pid) {
		fmt.Println("orcc proxy already running")
		return
	}

	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find executable: %v\n", err)
		os.Exit(1)
	}

	logF, err := os.OpenFile(logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open log: %v\n", err)
		os.Exit(1)
	}

	proc, err := os.StartProcess(bin, []string{bin, "serve"}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, logF, logF},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	logF.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "start serve: %v\n", err)
		os.Exit(1)
	}

	if err := daemon.WritePid(pidPath, proc.Pid); err != nil {
		fmt.Fprintf(os.Stderr, "write pid: %v\n", err)
		proc.Kill()
		os.Exit(1)
	}

	fmt.Println("orcc proxy started")
}

func serveProxy() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	p := proxy.New(cfg)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	log.Printf("starting proxy on %s", addr)
	if err := p.Start(addr); err != nil {
		log.Fatalf("proxy start: %v", err)
	}
}

func stopDaemon() {
	pidPath, err := daemon.PidPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	pid, err := daemon.ReadPid(pidPath)
	if err != nil || pid == 0 {
		fmt.Println("orcc proxy not running")
		return
	}

	if !daemon.IsRunning(pid) {
		fmt.Println("orcc proxy not running (stale pid file)")
		daemon.RemovePid(pidPath)
		return
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "kill %d: %v\n", pid, err)
		os.Exit(1)
	}

	daemon.RemovePid(pidPath)
	fmt.Println("orcc proxy stopped")
}

func printStatus() {
	pidPath, err := daemon.PidPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	pid, err := daemon.ReadPid(pidPath)
	if err != nil || pid == 0 {
		fmt.Println("orcc proxy: not running")
		return
	}

	if daemon.IsRunning(pid) {
		fmt.Printf("orcc proxy: running (pid %d)\n", pid)
	} else {
		fmt.Println("orcc proxy: not running (stale pid file)")
		daemon.RemovePid(pidPath)
	}
}

func runClaude(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if err := ensureProxyRunning(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	logDir, args := extractFlag(args, "--orcc-log-dir")
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating log dir %s: %v\n", logDir, err)
			os.Exit(1)
		}
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "claude not found in PATH: %v\n", err)
		os.Exit(1)
	}

	argv := runner.BuildArgs(cfg.DefaultModel, args)
	env := runner.BuildEnv(cfg, logDir)

	if err := syscall.Exec(claudeBin, argv, env); err != nil {
		fmt.Fprintf(os.Stderr, "exec claude: %v\n", err)
		os.Exit(1)
	}
}

func ensureProxyRunning(cfg *config.Config) error {
	pidPath, err := daemon.PidPath()
	if err != nil {
		return err
	}

	pid, err := daemon.ReadPid(pidPath)
	if err == nil && pid != 0 && daemon.IsRunning(pid) {
		return waitForProxy(cfg.Port)
	}

	startDaemon()
	return waitForProxy(cfg.Port)
}

func waitForProxy(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 40; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("proxy not ready after 2s")
}

func loadConfig() (*config.Config, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("get config path: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %v", err)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no api_key set in %s\nGet your key at https://openrouter.ai/keys", cfgPath)
	}
	if cfg.Port == 0 {
		cfg.Port = 3458
	}
	return cfg, nil
}

func logPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orcc", "orcc.log")
}

func extractFlag(args []string, flag string) (string, []string) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], append(args[:i:i], args[i+2:]...)
		}
	}
	return "", args
}