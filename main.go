package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

//go:embed static
var staticFiles embed.FS

var version = "dev"

var (
	cfgPort       int
	cfgCommands   []string
	cfgAttach     string
	cfgPassword   string
	cfgCols       int
	cfgRows       int
	cfgDaemon     bool
	cfgBufferSize int
	cfgBind       string
)

var rootCmd = &cobra.Command{
	Use:   "rc [flags]",
	Short: "Remote Control — run CLI commands in PTY, stream to browser",
	Long: `rc is a lightweight server that runs any CLI command in a pseudo-terminal (PTY)
and streams it to a web browser in real-time via WebSocket.

Examples:
  rc                                    Run default shell (bash)
  rc -c htop -c bash                    Multiple commands as tabs
  rc -p 9000 -c "python3 -i"           Custom port
  rc -a serverA:8000 -c bash            Agent mode: attach to remote hub
  rc --password secret -c bash          Password-protected server
  rc -d -c bash                         Run as daemon`,
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	f := rootCmd.Flags()
	f.IntVarP(&cfgPort, "port", "p", 8000, "HTTP server port")
	f.StringArrayVarP(&cfgCommands, "command", "c", nil, "Command to run (repeatable, e.g. -c bash -c htop)")
	f.StringVarP(&cfgAttach, "attach", "a", "", "Attach to remote rc hub (agent mode)")
	f.StringVar(&cfgPassword, "password", "", "Password for server access (env: RC_PASSWORD)")
	f.IntVar(&cfgCols, "cols", 120, "Initial terminal columns")
	f.IntVar(&cfgRows, "rows", 30, "Initial terminal rows")
	f.BoolVarP(&cfgDaemon, "daemon", "d", false, "Run as background daemon")
	f.IntVar(&cfgBufferSize, "buffer-size", 10, "Output buffer size in MB")
	f.StringVar(&cfgBind, "bind", "0.0.0.0", "Bind address")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Password from environment variable (fallback, avoids leaking in ps)
	if cfgPassword == "" {
		cfgPassword = os.Getenv("RC_PASSWORD")
	}

	// Daemon mode: re-exec self without -d/--daemon, fully detached
	if cfgDaemon {
		daemonize()
		return nil
	}

	// Default command
	if len(cfgCommands) == 0 {
		cfgCommands = []string{defaultShell}
	}

	bufferBytes := cfgBufferSize * 1024 * 1024

	// Agent mode: attach to a remote hub instead of running a server
	if cfgAttach != "" {
		RunAgent(cfgAttach, cfgCommands, uint16(cfgCols), uint16(cfgRows), cfgPassword, bufferBytes)
		return nil
	}

	// Create sessions (one per command)
	type session struct {
		Name   string
		PtyMgr *PTYManager
		Buf    *OutputBuffer
	}
	sessions := make([]session, len(cfgCommands))

	for i, cmd := range cfgCommands {
		parts := strings.Fields(cmd)
		cmdName := parts[0]
		var cmdArgs []string
		if len(parts) > 1 {
			cmdArgs = parts[1:]
		}

		buf := NewOutputBuffer(bufferBytes)
		ptyMgr, err := NewPTYManager(cmdName, cmdArgs, uint16(cfgCols), uint16(cfgRows), buf)
		if err != nil {
			return fmt.Errorf("failed to start PTY for '%s': %v", cmd, err)
		}
		sessions[i] = session{Name: cmd, PtyMgr: ptyMgr, Buf: buf}
	}
	defer func() {
		for _, s := range sessions {
			s.PtyMgr.Close()
		}
	}()

	// Build tab info for Hub
	tabNames := make([]string, len(sessions))
	ptyMgrs := make([]*PTYManager, len(sessions))
	bufs := make([]*OutputBuffer, len(sessions))
	for i, s := range sessions {
		tabNames[i] = s.Name
		ptyMgrs[i] = s.PtyMgr
		bufs[i] = s.Buf
	}

	hub := NewHub(ptyMgrs, bufs, tabNames)
	for i, s := range sessions {
		hub.StartOutputPump(i, s.PtyMgr.OutputChan())
	}

	// Routes
	mux := http.NewServeMux()

	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("/ws", requireAuth(cfgPassword, hub.HandleWebSocket))
	mux.HandleFunc("/attach", requireAuth(cfgPassword, hub.HandleAttach))

	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	mux.HandleFunc("/info", requireAuth(cfgPassword, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hostname":  hostname,
			"workspace": workspace,
			"commands":  cfgCommands,
		})
	}))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		hub.mu.RLock()
		totalTabs := len(hub.tabs)
		running := 0
		for _, t := range hub.tabs {
			if t.Status == "running" {
				running++
			}
		}
		hub.mu.RUnlock()
		fmt.Fprintf(w, `{"status":"ok","tabs":%d,"running":%d}`, totalTabs, running)
	})

	addr := fmt.Sprintf("%s:%d", cfgBind, cfgPort)
	server := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		for _, s := range sessions {
			s.PtyMgr.Close()
		}
		server.Close()
	}()

	log.Printf("rc running on http://%s", addr)
	for i, cmd := range cfgCommands {
		log.Printf("  Tab %d: %s", i, cmd)
	}
	if cfgPassword != "" {
		log.Printf("  Password protection: enabled")
	}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %v", err)
	}
	return nil
}

// daemonize re-executes the current binary without -d/--daemon, fully detached.
func daemonize() {
	args := []string{}
	for _, arg := range os.Args[1:] {
		if arg == "-d" || arg == "--daemon" {
			continue
		}
		if strings.HasPrefix(arg, "--daemon=") {
			continue
		}
		args = append(args, arg)
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir, _ = os.Getwd()
	cmd.Env = os.Environ()

	// Detach: new session / new process group
	cmd.SysProcAttr = daemonSysProcAttr()

	// Redirect stdout/stderr to log file
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("rc-%d.log", os.Getpid()))
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start daemon: %v", err)
	}

	fmt.Printf("rc daemon started (pid=%d, log=%s)\n", cmd.Process.Pid, logPath)
	os.Exit(0)
}

