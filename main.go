package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
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
	flag.Parse()

	if *command == "" {
		// Default: try copilot, fall back to bash
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

func lookPath(cmd string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		path := dir + "/" + cmd
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", cmd)
}
