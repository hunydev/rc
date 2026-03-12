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

func main() {
	port := flag.Int("port", 8000, "HTTP server port")
	command := flag.String("command", "", "Command to run in PTY (e.g. 'copilot --yolo')")
	cols := flag.Int("cols", 120, "Initial terminal columns")
	rows := flag.Int("rows", 30, "Initial terminal rows")
	daemon := flag.Bool("daemon", false, "Run as background daemon")
	flag.Parse()

	// Daemon mode: re-exec self without -daemon, detached
	if *daemon {
		daemonize()
		return
	}

	if *command == "" {
		if _, err := lookPath("copilot"); err == nil {
			*command = "copilot"
		} else {
			*command = "bash"
		}
	}

	parts := strings.Fields(*command)
	cmdName := parts[0]
	var cmdArgs []string
	if len(parts) > 1 {
		cmdArgs = parts[1:]
	}

	// Create output buffer (10MB)
	buf := NewOutputBuffer(10 * 1024 * 1024)

	// Create PTY manager
	ptyMgr, err := NewPTYManager(cmdName, cmdArgs, uint16(*cols), uint16(*rows), buf)
	if err != nil {
		log.Fatalf("Failed to start PTY: %v", err)
	}
	defer ptyMgr.Close()

	// Create WebSocket hub
	hub := NewHub(ptyMgr, buf)

	// Start broadcasting PTY output to WebSocket clients
	hub.StartOutputPump(ptyMgr.OutputChan())

	// Routes
	mux := http.NewServeMux()

	// Static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// WebSocket
	mux.HandleFunc("/ws", hub.HandleWebSocket)

	// Server info (hostname, workspace, command) for header display
	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"hostname":  hostname,
			"workspace": workspace,
			"command":   *command,
		})
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","pid":%d,"running":%t}`, ptyMgr.Pid(), ptyMgr.IsRunning())
	})

	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		ptyMgr.Close()
		server.Close()
	}()

	log.Printf("🚀 rc running on http://localhost%s", addr)
	log.Printf("📺 Command: %s %s", cmdName, strings.Join(cmdArgs, " "))
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
