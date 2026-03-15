package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Agent struct {
	target    string
	sessions  []*agentSession
	password  string
	noRestart bool
	readonly  bool
	upload    bool
	mu        sync.RWMutex
	writeCh   chan []byte
}

type agentSession struct {
	Name   string
	PtyMgr *PTYManager
	Buf    *OutputBuffer
}

// RunAgent starts rc in agent mode, attaching local PTYs to a remote hub.
func RunAgent(target string, commands []string, labels []string, cols, rows uint16, password string, bufferSize int, noRestart, readonly, upload bool, extraEnv []string) {
	hostname, _ := os.Hostname()

	sessions := make([]*agentSession, len(commands))
	for i, cmd := range commands {
		cmdName, cmdArgs := parseCommand(cmd)

		buf := NewOutputBuffer(bufferSize)
		ptyMgr, err := NewPTYManager(cmdName, cmdArgs, cols, rows, buf, extraEnv)
		if err != nil {
			log.Fatalf("Failed to start PTY for '%s': %v", cmd, err)
		}

		var name string
		if i < len(labels) && labels[i] != "" {
			name = fmt.Sprintf("%s: %s", hostname, labels[i])
		} else {
			name = fmt.Sprintf("%s: %s", hostname, cmd)
		}
		sessions[i] = &agentSession{Name: name, PtyMgr: ptyMgr, Buf: buf}
	}

	agent := &Agent{
		target:    buildAttachURL(target),
		sessions:  sessions,
		password:  password,
		noRestart: noRestart,
		readonly:  readonly,
		upload:    upload,
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		signal.Stop(sigChan)
		log.Println("Agent shutting down...")
		for _, s := range sessions {
			s.PtyMgr.Close()
		}
		os.Exit(0)
	}()

	// Start persistent output drain goroutines.
	for i, s := range sessions {
		idx, sess := i, s
		safeGo("drainOutput", func() { agent.drainOutput(idx, sess) })
	}

	log.Printf("Agent mode: attaching to %s", agent.target)
	for _, s := range sessions {
		log.Printf("  Command: %s", s.Name)
	}

	// Connection loop with exponential backoff
	retryDelay := 1 * time.Second
	const maxRetryDelay = 30 * time.Second
	for {
		err := agent.connect()
		if err != nil {
			log.Printf("Hub connection error: %v", err)
		}
		log.Printf("Reconnecting in %v...", retryDelay)
		time.Sleep(retryDelay)
		retryDelay = retryDelay * 2
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
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
	// Preserve existing path as route prefix, append /attach
	u.Path = strings.TrimRight(u.Path, "/") + "/attach"
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
		msg := mustMarshal(WSMessage{Type: "output", Data: string(data), Tab: idx})
		a.sendToHub(msg)
	}
	msg := mustMarshal(WSMessage{Type: "status", Data: "exited", Tab: idx})
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

	writeCh := make(chan []byte, channelBufSize)

	// Writer goroutine with ping (serializes WebSocket writes)
	writerDone := make(chan struct{})
	safeGo("agentWriter", func() {
		defer close(writerDone)
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-writeCh:
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	})

	// Register with hub
	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	currentUser := ""
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}
	tabInfos := make([]TabInfo, len(a.sessions))
	for i, s := range a.sessions {
		tabInfos[i] = TabInfo{
			Name:      s.Name,
			Hostname:  hostname,
			Workspace: workspace,
			NoRestart: a.noRestart,
			Readonly:  a.readonly,
			Upload:    a.upload,
		}
	}
	regMsg := mustMarshal(WSMessage{Type: "register", Data: currentUser, Tabs: tabInfos})
	writeCh <- regMsg

	// Send buffer snapshots and current status for each session
	// (so hub can replay to browsers that are already connected)
	for i, s := range a.sessions {
		snapshot := s.Buf.Snapshot()
		if len(snapshot) > 0 {
			msg := mustMarshal(WSMessage{Type: "output", Data: string(snapshot), Tab: i})
			writeCh <- msg
		}
		status := "exited"
		if s.PtyMgr.IsRunning() {
			status = "running"
		}
		statusMsg := mustMarshal(WSMessage{Type: "status", Data: status, Tab: i})
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

	// Read commands from hub with pong-based keepalive
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Hub message parse error: %v", err)
			continue
		}

		tab := msg.Tab
		if tab < 0 || tab >= len(a.sessions) {
			continue
		}

		switch msg.Type {
		case "input":
			if a.readonly {
				continue
			}
			if err := a.sessions[tab].PtyMgr.Write([]byte(msg.Data)); err != nil {
				log.Printf("Agent PTY write error (tab %d): %v", tab, err)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if err := a.sessions[tab].PtyMgr.Resize(msg.Cols, msg.Rows); err != nil {
					log.Printf("Agent PTY resize error (tab %d): %v", tab, err)
				}
			}
		case "restart":
			if a.noRestart {
				continue
			}
			outputCh, err := a.sessions[tab].PtyMgr.Restart()
			if err != nil {
				errMsg := mustMarshal(WSMessage{Type: "error", Data: err.Error(), Tab: tab})
				a.sendToHub(errMsg)
				continue
			}
			statusMsg := mustMarshal(WSMessage{Type: "status", Data: "restarted", Tab: tab})
			a.sendToHub(statusMsg)
			// Start new drain goroutine for the restarted process
			_ = outputCh
			safeGo("drainOutput", func() { a.drainOutput(tab, a.sessions[tab]) })

		case "upload":
			result := a.handleUpload(msg.Filename, msg.Data)
			result.Tab = tab
			a.sendToHub(mustMarshal(result))
		}
	}
}

// handleUpload processes an upload message from the hub and writes the file to the agent's wd.
func (a *Agent) handleUpload(filename, b64data string) WSMessage {
if !a.upload {
return WSMessage{Type: "upload_result", Cols: 403, Data: `{"error":"upload not enabled"}`}
}

filename = filepath.Base(filename)
if filename == "." || filename == "/" || filename == ".." {
return WSMessage{Type: "upload_result", Cols: 400, Data: `{"error":"invalid filename"}`}
}

data, err := base64.StdEncoding.DecodeString(b64data)
if err != nil {
return WSMessage{Type: "upload_result", Cols: 400, Data: `{"error":"invalid file data"}`}
}

wd, err := os.Getwd()
if err != nil {
return WSMessage{Type: "upload_result", Cols: 500, Data: `{"error":"failed to get working directory"}`}
}
realWd, err := filepath.EvalSymlinks(wd)
if err != nil {
return WSMessage{Type: "upload_result", Cols: 500, Data: `{"error":"failed to resolve working directory"}`}
}
destPath := filepath.Join(realWd, filename)

if !strings.HasPrefix(destPath, realWd+string(filepath.Separator)) {
return WSMessage{Type: "upload_result", Cols: 400, Data: `{"error":"invalid file path"}`}
}

if _, err := os.Stat(destPath); err == nil {
body := mustMarshal(map[string]string{"error": "file already exists: " + filename})
return WSMessage{Type: "upload_result", Cols: 409, Data: string(body)}
}

if err := os.WriteFile(destPath, data, 0644); err != nil {
body := mustMarshal(map[string]string{"error": "failed to write file: " + err.Error()})
return WSMessage{Type: "upload_result", Cols: 500, Data: string(body)}
}

log.Printf("File uploaded: %s (%d bytes)", destPath, len(data))
body := mustMarshal(map[string]interface{}{"path": destPath, "name": filename, "size": len(data)})
return WSMessage{Type: "upload_result", Data: string(body)}
}
