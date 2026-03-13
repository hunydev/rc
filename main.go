package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

//go:embed static
var staticFiles embed.FS

// multiFlag allows repeated -c flags
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func main() {
	port := flag.Int("port", 8000, "HTTP server port")
	command := flag.String("command", "", "Command to run in PTY (single command, for backwards compat)")
	attach := flag.String("attach", "", "Attach to remote rc hub (e.g. 'serverA:8000')")
	var commands multiFlag
	flag.Var(&commands, "c", "Command to run in PTY (repeatable, e.g. -c 'bash' -c 'htop')")
	cols := flag.Int("cols", 120, "Initial terminal columns")
	rows := flag.Int("rows", 30, "Initial terminal rows")
	daemon := flag.Bool("daemon", false, "Run as background daemon")
	flag.Parse()

	// Daemon mode: re-exec self without -daemon, detached
	if *daemon {
		daemonize()
		return
	}

	// Resolve command list: -c flags take priority, then --command, then default
	if len(commands) == 0 {
		if *command != "" {
			commands = []string{*command}
		} else {
			if _, err := lookPath("copilot"); err == nil {
				commands = []string{"copilot"}
			} else {
				commands = []string{"bash"}
			}
		}
	}

	// Agent mode: attach to a remote hub instead of running a server
	if *attach != "" {
		RunAgent(*attach, commands, uint16(*cols), uint16(*rows))
		return
	}

	// Create sessions (one per command)
	type session struct {
		Name   string
		PtyMgr *PTYManager
		Buf    *OutputBuffer
	}
	sessions := make([]session, len(commands))

	for i, cmd := range commands {
		parts := strings.Fields(cmd)
		cmdName := parts[0]
		var cmdArgs []string
		if len(parts) > 1 {
			cmdArgs = parts[1:]
		}

		buf := NewOutputBuffer(10 * 1024 * 1024)
		ptyMgr, err := NewPTYManager(cmdName, cmdArgs, uint16(*cols), uint16(*rows), buf)
		if err != nil {
			log.Fatalf("Failed to start PTY for '%s': %v", cmd, err)
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

	mux.HandleFunc("/ws", hub.HandleWebSocket)
	mux.HandleFunc("/attach", hub.HandleAttach)

	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hostname":  hostname,
			"workspace": workspace,
			"commands":  commands,
		})
	})

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

	addr := fmt.Sprintf(":%d", *port)
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

	log.Printf("rc running on http://localhost%s", addr)
	for i, cmd := range commands {
		log.Printf("  Tab %d: %s", i, cmd)
	}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// daemonize re-executes the current binary without -daemon, fully detached.
func daemonize() {
	args := []string{}
	skipNext := false
	for i, arg := range os.Args[1:] {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "-daemon" || arg == "--daemon" {
			continue
		}
		// Handle "-daemon=true" or "--daemon=true"
		if strings.HasPrefix(arg, "-daemon=") || strings.HasPrefix(arg, "--daemon=") {
			continue
		}
		_ = i
		args = append(args, arg)
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir, _ = os.Getwd()
	cmd.Env = os.Environ()

	// Detach: new session, no controlling terminal
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Redirect stdout/stderr to log file
	logPath := fmt.Sprintf("/tmp/rc-%d.log", os.Getpid())
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

func lookPath(cmd string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		path := dir + "/" + cmd
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", cmd)
}
