package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Agent struct {
	target   string
	sessions []*agentSession
	password string
	mu       sync.RWMutex
	writeCh  chan []byte
}

type agentSession struct {
	Name   string
	PtyMgr *PTYManager
	Buf    *OutputBuffer
}

// RunAgent starts rc in agent mode, attaching local PTYs to a remote hub.
func RunAgent(target string, commands []string, cols, rows uint16, password string, bufferSize int) {
	hostname, _ := os.Hostname()

	sessions := make([]*agentSession, len(commands))
	for i, cmd := range commands {
		parts := strings.Fields(cmd)
		cmdName := parts[0]
		var cmdArgs []string
		if len(parts) > 1 {
			cmdArgs = parts[1:]
		}

		buf := NewOutputBuffer(bufferSize)
		ptyMgr, err := NewPTYManager(cmdName, cmdArgs, cols, rows, buf)
		if err != nil {
			log.Fatalf("Failed to start PTY for '%s': %v", cmd, err)
		}

		name := fmt.Sprintf("%s: %s", hostname, cmd)
		sessions[i] = &agentSession{Name: name, PtyMgr: ptyMgr, Buf: buf}
	}

	agent := &Agent{
		target:   buildAttachURL(target),
		sessions: sessions,
		password: password,
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Agent shutting down...")
		for _, s := range sessions {
			s.PtyMgr.Close()
		}
		os.Exit(0)
	}()

	// Start persistent output drain goroutines.
	// These always read from outputCh so the PTY readLoop never blocks.
	// When not connected to hub, output is discarded (kept in OutputBuffer).
	for i, s := range sessions {
		go agent.drainOutput(i, s)
	}

	log.Printf("Agent mode: attaching to %s", agent.target)
	for _, s := range sessions {
		log.Printf("  Command: %s", s.Name)
	}

	// Connection loop with retry
	for {
		err := agent.connect()
		if err != nil {
			log.Printf("Hub connection error: %v", err)
		}
		log.Println("Reconnecting in 3 seconds...")
		time.Sleep(3 * time.Second)
	}
}

func buildAttachURL(target string) string {
	if !strings.Contains(target, "://") {
		// Auto-detect: if port is 443 or absent (implies default HTTPS), use wss
		host := target
		scheme := "wss"
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			port := host[idx+1:]
			if port != "443" {
				scheme = "ws"
			}
		}
		target = scheme + "://" + target
	}
	u, err := url.Parse(target)
	if err != nil {
		log.Fatalf("Invalid attach target: %v", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "/attach"
	return u.String()
}

// sendToHub sends a message to the hub if connected, discards otherwise.
func (a *Agent) sendToHub(msg []byte) {
	a.mu.RLock()
	ch := a.writeCh
	a.mu.RUnlock()
	if ch != nil {
		select {
		case ch <- msg:
		default:
		}
	}
}

// drainOutput persistently reads PTY output and forwards to hub.
// Runs for the lifetime of the PTY process. Exits when outputCh closes (process exit).
func (a *Agent) drainOutput(idx int, s *agentSession) {
	for data := range s.PtyMgr.OutputChan() {
		msg, _ := json.Marshal(WSMessage{Type: "output", Data: string(data), Tab: idx})
		a.sendToHub(msg)
	}
	msg, _ := json.Marshal(WSMessage{Type: "status", Data: "exited", Tab: idx})
	a.sendToHub(msg)
}

// connect establishes a single connection to the hub.
// Returns when the connection is lost.
func (a *Agent) connect() error {
	header := http.Header{}
	if a.password != "" {
		header.Set("Authorization", "Bearer "+a.password)
	}
	conn, _, err := websocket.DefaultDialer.Dial(a.target, header)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	writeCh := make(chan []byte, 256)

	// Writer goroutine (serializes WebSocket writes)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for msg := range writeCh {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Register with hub
	tabInfos := make([]TabInfo, len(a.sessions))
	for i, s := range a.sessions {
		tabInfos[i] = TabInfo{Name: s.Name}
	}
	regMsg, _ := json.Marshal(WSMessage{Type: "register", Tabs: tabInfos})
	writeCh <- regMsg

	// Send buffer snapshots and current status for each session
	// (so hub can replay to browsers that are already connected)
	for i, s := range a.sessions {
		snapshot := s.Buf.Snapshot()
		if len(snapshot) > 0 {
			msg, _ := json.Marshal(WSMessage{Type: "output", Data: string(snapshot), Tab: i})
			writeCh <- msg
		}
		status := "exited"
		if s.PtyMgr.IsRunning() {
			status = "running"
		}
		statusMsg, _ := json.Marshal(WSMessage{Type: "status", Data: status, Tab: i})
		writeCh <- statusMsg
	}

	// Activate drain goroutines to forward new output through this connection
	a.mu.Lock()
	a.writeCh = writeCh
	a.mu.Unlock()

	log.Printf("Attached to hub (%d tabs)", len(a.sessions))

	// Deactivate drain goroutines on exit
	defer func() {
		a.mu.Lock()
		a.writeCh = nil
		a.mu.Unlock()
		close(writeCh)
	}()

	// Read commands from hub (input, resize, restart)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		tab := msg.Tab
		if tab < 0 || tab >= len(a.sessions) {
			continue
		}

		switch msg.Type {
		case "input":
			a.sessions[tab].PtyMgr.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				a.sessions[tab].PtyMgr.Resize(msg.Cols, msg.Rows)
			}
		case "restart":
			outputCh, err := a.sessions[tab].PtyMgr.Restart()
			if err != nil {
				errMsg, _ := json.Marshal(WSMessage{Type: "error", Data: err.Error(), Tab: tab})
				a.sendToHub(errMsg)
				continue
			}
			statusMsg, _ := json.Marshal(WSMessage{Type: "status", Data: "restarted", Tab: tab})
			a.sendToHub(statusMsg)
			// Start new drain goroutine for the restarted process
			_ = outputCh // drainOutput will call OutputChan() which returns the new channel
			go a.drainOutput(tab, a.sessions[tab])
		}
	}
}
