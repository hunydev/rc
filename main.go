package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

//go:embed static
var staticFiles embed.FS

var version = "dev"

const (
	maxUploadSize     = 100 * 1024 * 1024 // 100 MB
	httpReadTimeout   = 30 * time.Second
	httpWriteTimeout  = 0 // disabled: WebSocket requires long-lived writes
	httpIdleTimeout   = 120 * time.Second
	shutdownTimeout   = 5 * time.Second
)

var (
	cfgPort       int
	cfgCommands   []string
	cfgLabels     []string
	cfgAttach     string
	cfgPassword   string
	cfgCols       int
	cfgRows       int
	cfgDaemon     bool
	cfgBufferSize int
	cfgBind       string
	cfgNoRestart  bool
	cfgReadonly   bool
	cfgRoute      string
	cfgUpload     bool
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
	f.StringArrayVarP(&cfgLabels, "label", "l", nil, "Tab label (repeatable, matches -c order, e.g. -l 'My Shell')")
	f.StringVarP(&cfgAttach, "attach", "a", "", "Attach to remote rc hub (agent mode)")
	f.StringVar(&cfgPassword, "password", "", "Password for server access (env: RC_PASSWORD)")
	f.IntVar(&cfgCols, "cols", 120, "Initial terminal columns")
	f.IntVar(&cfgRows, "rows", 30, "Initial terminal rows")
	f.BoolVarP(&cfgDaemon, "daemon", "d", false, "Run as background daemon")
	f.IntVar(&cfgBufferSize, "buffer-size", 10, "Output buffer size in MB")
	f.StringVar(&cfgBind, "bind", "0.0.0.0", "Bind address")
	f.BoolVar(&cfgNoRestart, "no-restart", false, "Disable command restart after exit")
	f.BoolVar(&cfgReadonly, "readonly", false, "Disable stdin input (output only)")
	f.StringVar(&cfgRoute, "route", "", "URL route prefix (e.g. /myapp)")
	f.BoolVar(&cfgUpload, "upload", false, "Enable file upload to working directory")
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

	// Normalize route prefix: ensure leading slash, strip trailing slash
	cfgRoute = strings.TrimRight(cfgRoute, "/")
	if cfgRoute != "" && !strings.HasPrefix(cfgRoute, "/") {
		cfgRoute = "/" + cfgRoute
	}

	// Agent mode: attach to a remote hub instead of running a server
	if cfgAttach != "" {
		RunAgent(cfgAttach, cfgCommands, cfgLabels, uint16(cfgCols), uint16(cfgRows), cfgPassword, bufferBytes, cfgNoRestart, cfgReadonly, cfgUpload)
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
		cmdName, cmdArgs := parseCommand(cmd)

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
		if i < len(cfgLabels) && cfgLabels[i] != "" {
			tabNames[i] = cfgLabels[i]
		} else {
			tabNames[i] = s.Name
		}
		ptyMgrs[i] = s.PtyMgr
		bufs[i] = s.Buf
	}

	currentUser := ""
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}
	hub := NewHub(ptyMgrs, bufs, tabNames, currentUser, cfgNoRestart, cfgReadonly, cfgUpload)
	for i, s := range sessions {
		hub.StartOutputPump(i, s.PtyMgr.OutputChan())
	}

	// Routes
	mux := http.NewServeMux()
	rp := cfgRoute // route prefix

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to load static files: %v", err)
	}
	if rp == "" {
		mux.Handle("/", http.FileServer(http.FS(staticFS)))
	} else {
		mux.Handle(rp+"/", http.StripPrefix(rp, http.FileServer(http.FS(staticFS))))
	}

	mux.HandleFunc(rp+"/ws", requireAuth(cfgPassword, hub.HandleWebSocket))
	mux.HandleFunc(rp+"/attach", requireAuth(cfgPassword, hub.HandleAttach))

	mux.HandleFunc(rp+"/info", requireAuth(cfgPassword, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"hostname":  hub.hostname,
			"workspace": hub.workspace,
			"commands":  cfgCommands,
			"route":     rp,
			"upload":    cfgUpload,
			"version":   version,
		}); err != nil {
			log.Printf("Failed to encode /info response: %v", err)
		}
	}))

	mux.HandleFunc(rp+"/health", func(w http.ResponseWriter, r *http.Request) {
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

	if cfgUpload {
		mux.HandleFunc(rp+"/upload", requireAuth(cfgPassword, hub.HandleUpload))
	}

	addr := fmt.Sprintf("%s:%d", cfgBind, cfgPort)
	server := &http.Server{
		Addr:        addr,
		Handler:     securityHeaders(mux),
		ReadTimeout: httpReadTimeout,
		IdleTimeout: httpIdleTimeout,
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		for _, s := range sessions {
			s.PtyMgr.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		server.Shutdown(ctx)
		// Force exit after graceful shutdown completes
		os.Exit(0)
	}()

	log.Printf("rc running on http://%s%s", addr, rp+"/")
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
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
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
		logFile.Close()
		log.Fatalf("Failed to start daemon: %v", err)
	}
	logFile.Close()

	fmt.Printf("rc daemon started (pid=%d, log=%s)\n", cmd.Process.Pid, logPath)
	os.Exit(0)
}

// securityHeaders adds standard security headers to all HTTP responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// parseCommand splits a command string respecting single/double quotes and backslash escaping.
func parseCommand(s string) (string, []string) {
	var args []string
	var cur strings.Builder
	inSingle, inDouble, escaped := false, false, false

	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if (r == ' ' || r == '\t') && !inSingle && !inDouble {
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}

	if len(args) == 0 {
		return s, nil
	}
	return args[0], args[1:]
}

