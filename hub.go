package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10 // 54s

	channelBufSize    = 256
	readBufSize       = 4096
	remoteTabBufBytes = 10 * 1024 * 1024 // 10 MB
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (curl, agents)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// extractIP returns the client IP from the request, respecting proxy headers when trustedProxy is enabled.
func extractIP(r *http.Request) string {
	if trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For may contain multiple IPs; take the first (original client)
			if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
				return ip
			}
		}
		if xri := r.Header.Get("X-Real-Ip"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		return r.RemoteAddr
	}
	return host
}

// TabEntry represents a terminal session, either local or remote (agent-backed).
type TabEntry struct {
	Name      string
	Command   string
	Remote    bool
	Removed   bool
	Buf       *OutputBuffer
	Status    string // "running", "exited", "disconnected"
	User      string
	Hostname  string
	Workspace string
	Addr      string // remote agent IP
	NoRestart bool
	Readonly  bool
	Upload    bool

	// For local tabs (nil for remote tabs)
	PtyMgr *PTYManager

	// For remote tabs (nil for local tabs)
	agent    *AgentConn
	agentTab int // tab index relative to the agent
}

// AgentConn represents a connected remote agent.
type AgentConn struct {
	conn    *websocket.Conn
	send    chan []byte
	baseTab int // first global tab index for this agent
	numTabs int // number of tabs this agent owns
}

// safeSend sends a message to a channel, recovering from panics on closed channels.
func safeSend(ch chan<- []byte, msg []byte) {
	defer func() { recover() }()
	select {
	case ch <- msg:
	default:
	}
}

// safeGo runs fn in a goroutine with panic recovery.
func safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered in %s: %v", name, r)
			}
		}()
		fn()
	}()
}

// mustMarshal marshals v to JSON, logging on failure.
func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("JSON marshal error: %v", err)
		return nil
	}
	return data
}

// Hub manages browser clients, local PTYs, and remote agents.
type Hub struct {
	tabs           []*TabEntry
	clients        map[*Client]bool
	agents         map[*AgentConn]bool
	hostname       string
	workspace      string
	user           string
	noRestart      bool
	readonly       bool
	maxConnections int
	mu             sync.RWMutex
	pendingUploads map[int]chan uploadResult // keyed by hub tab index
	uploadMu       sync.Mutex
}

// uploadResult carries the agent's upload response back to the HTTP handler.
type uploadResult struct {
	Status int    // HTTP status code
	Body   string // JSON response body
}

// Client represents a connected browser WebSocket client.
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// WSMessage is the JSON message format for all WebSocket communication.
type WSMessage struct {
	Type     string    `json:"type"`
	Data     string    `json:"data,omitempty"`
	Cols     uint16    `json:"cols,omitempty"`
	Rows     uint16    `json:"rows,omitempty"`
	Tab      int       `json:"tab"`
	Tabs     []TabInfo `json:"tabs,omitempty"`
	Remote   bool      `json:"remote,omitempty"`
	Meta     *TabInfo  `json:"meta,omitempty"`
	Filename string    `json:"filename,omitempty"`
}

// TabInfo describes a tab in the 'tabs' message.
type TabInfo struct {
	Name      string `json:"name"`
	Command   string `json:"command,omitempty"`
	Remote    bool   `json:"remote,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Pid       int    `json:"pid,omitempty"`
	User      string `json:"user,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Addr      string `json:"addr,omitempty"`
	NoRestart bool   `json:"noRestart,omitempty"`
	Readonly  bool   `json:"readonly,omitempty"`
	Upload    bool   `json:"upload,omitempty"`
}

// NewHub creates a new Hub with local PTY sessions.
func NewHub(ptyMgrs []*PTYManager, bufs []*OutputBuffer, tabNames, commands []string, currentUser string, noRestart, readonly, upload bool, maxConnections int) *Hub {
	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	h := &Hub{
		tabs:           make([]*TabEntry, len(ptyMgrs)),
		clients:        make(map[*Client]bool),
		agents:         make(map[*AgentConn]bool),
		hostname:       hostname,
		workspace:      workspace,
		user:           currentUser,
		noRestart:      noRestart,
		readonly:       readonly,
		maxConnections: maxConnections,
		pendingUploads: make(map[int]chan uploadResult),
	}
	for i := range ptyMgrs {
		cmd := ""
		if i < len(commands) {
			cmd = commands[i]
		}
		h.tabs[i] = &TabEntry{
			Name:      tabNames[i],
			Command:   cmd,
			Buf:       bufs[i],
			Status:    "running",
			PtyMgr:    ptyMgrs[i],
			User:      currentUser,
			Hostname:  hostname,
			Workspace: workspace,
			NoRestart: noRestart,
			Readonly:  readonly,
			Upload:    upload,
		}
	}
	return h
}

func (h *Hub) getTabInfos() []TabInfo {
	infos := make([]TabInfo, len(h.tabs))
	for i, t := range h.tabs {
		infos[i] = TabInfo{
			Name:      t.Name,
			Command:   t.Command,
			Remote:    t.Remote,
			Removed:   t.Removed,
			User:      t.User,
			Hostname:  t.Hostname,
			Workspace: t.Workspace,
			Addr:      t.Addr,
			NoRestart: t.NoRestart,
			Readonly:  t.Readonly,
			Upload:    t.Upload,
		}
		if t.PtyMgr != nil {
			infos[i].Pid = t.PtyMgr.Pid()
		}
	}
	return infos
}

// ClientCount returns the number of connected browser clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends PTY output to all browser clients (for local tabs).
func (h *Hub) Broadcast(tab int, data []byte) {
	msg := mustMarshal(WSMessage{Type: "output", Data: string(data), Tab: tab})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		safeSend(client.send, msg)
	}
}

// BroadcastStatus sends a status update for a tab to all browser clients.
func (h *Hub) BroadcastStatus(tab int, status string) {
	h.mu.Lock()
	if tab >= 0 && tab < len(h.tabs) {
		h.tabs[tab].Status = status
	}
	h.mu.Unlock()

	msg := mustMarshal(WSMessage{Type: "status", Data: status, Tab: tab})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		safeSend(client.send, msg)
	}
}

// broadcastToClients sends a raw JSON message to all browser clients.
func (h *Hub) broadcastToClients(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		safeSend(client.send, msg)
	}
}

// StartOutputPump reads from a local PTY's output channel and broadcasts to clients.
func (h *Hub) StartOutputPump(tab int, outputCh <-chan []byte) {
	safeGo("outputPump", func() {
		for data := range outputCh {
			h.Broadcast(tab, data)
		}
		h.Broadcast(tab, []byte("\r\n\033[1;31m[rc] Process exited.\033[0m\r\n"))
		h.BroadcastStatus(tab, "exited")
	})
}

const uploadProxyTimeout = 60 * time.Second

// HandleUpload handles file upload — local or proxied to agent.
func (h *Hub) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// CSRF protection: reject requests with a mismatched Origin header
	if origin := r.Header.Get("Origin"); origin != "" {
		host := r.Host
		if host == "" {
			host = r.Header.Get("Host")
		}
		allowed := "http://" + host
		allowedTLS := "https://" + host
		if origin != allowed && origin != allowedTLS {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"origin not allowed"}`))
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Determine target tab
	tabStr := r.URL.Query().Get("tab")
	tab := -1
	if tabStr != "" {
		if v, err := strconv.Atoi(tabStr); err == nil {
			tab = v
		}
	}

	// Check if target tab is a remote agent tab
	var agent *AgentConn
	var agentTab int
	if tab >= 0 {
		h.mu.RLock()
		if tab < len(h.tabs) && h.tabs[tab].Remote && h.tabs[tab].agent != nil && h.tabs[tab].Upload {
			agent = h.tabs[tab].agent
			agentTab = h.tabs[tab].agentTab
		}
		h.mu.RUnlock()
	}

	// Read the uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read file: " + err.Error()})
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	if filename == "." || filename == "/" || filename == ".." {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid filename"})
		return
	}

	if agent != nil {
		// Proxy upload to agent via WebSocket
		data, err := io.ReadAll(file)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to read file: " + err.Error()})
			return
		}

		// Register pending upload
		ch := make(chan uploadResult, 1)
		h.uploadMu.Lock()
		h.pendingUploads[tab] = ch
		h.uploadMu.Unlock()

		// Send upload message to agent
		uploadMsg := mustMarshal(WSMessage{
			Type:     "upload",
			Data:     base64.StdEncoding.EncodeToString(data),
			Filename: filename,
			Tab:      agentTab,
		})
		safeSend(agent.send, uploadMsg)

		// Wait for agent response
		select {
		case result := <-ch:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(result.Status)
			fmt.Fprint(w, result.Body)
		case <-time.After(uploadProxyTimeout):
			h.uploadMu.Lock()
			delete(h.pendingUploads, tab)
			h.uploadMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGatewayTimeout)
			json.NewEncoder(w).Encode(map[string]string{"error": "agent upload timeout"})
		}
		return
	}

	// Local upload
	wd, err := os.Getwd()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get working directory"})
		return
	}
	realWd, err := filepath.EvalSymlinks(wd)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve working directory"})
		return
	}
	destPath := filepath.Join(realWd, filename)

	if !strings.HasPrefix(destPath, realWd+string(filepath.Separator)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid file path"})
		return
	}

	if _, err := os.Stat(destPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "file already exists: " + filename})
		return
	}

	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create file: " + err.Error()})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		if rmErr := os.Remove(destPath); rmErr != nil {
			log.Printf("Failed to cleanup partial upload: %v", rmErr)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file: " + err.Error()})
		return
	}

	log.Printf("File uploaded: %s (%d bytes)", destPath, written)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path": destPath,
		"name": filename,
		"size": written,
	})
}

// HandleWebSocket handles browser WebSocket connections.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Echo auth subprotocol in upgrade response
	responseHeader := http.Header{}
	for _, proto := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		proto = strings.TrimSpace(proto)
		if strings.HasPrefix(proto, "auth-") {
			responseHeader.Set("Sec-WebSocket-Protocol", proto)
			break
		}
	}

	// Enforce max connections limit
	if h.maxConnections > 0 {
		h.mu.RLock()
		current := len(h.clients)
		h.mu.RUnlock()
		if current >= h.maxConnections {
			http.Error(w, "maximum connections reached", http.StatusServiceUnavailable)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, channelBufSize),
	}

	// Atomically add client and snapshot tab state to avoid missing tab_added messages
	h.mu.Lock()
	h.clients[client] = true
	tabInfos := h.getTabInfos()
	tabCount := len(h.tabs)
	snapshots := make([][]byte, tabCount)
	statuses := make([]string, tabCount)
	for i := 0; i < tabCount; i++ {
		if h.tabs[i].Removed {
			continue
		}
		snapshots[i] = h.tabs[i].Buf.Snapshot()
		statuses[i] = h.tabs[i].Status
	}
	h.mu.Unlock()

	log.Printf("Client connected: %s (total: %d)", r.RemoteAddr, len(h.clients))

	// Send tab list
	tabsMsg := mustMarshal(WSMessage{Type: "tabs", Tabs: tabInfos})
	client.send <- tabsMsg

	// Send history and status for each tab
	for i := 0; i < tabCount; i++ {
		if statuses[i] == "" {
			continue // removed tab
		}
		if len(snapshots[i]) > 0 {
			histMsg := mustMarshal(WSMessage{Type: "output", Data: string(snapshots[i]), Tab: i})
			client.send <- histMsg
		}
		statusMsg := mustMarshal(WSMessage{Type: "status", Data: statuses[i], Tab: i})
		client.send <- statusMsg
	}

	safeGo("writePump", func() { h.writePump(client) })
	safeGo("readPump", func() { h.readPump(client) })
}

func (h *Hub) writePump(c *Client) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) readPump(c *Client) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		close(c.send)
		c.conn.Close()
		log.Printf("Client disconnected (remaining: %d)", len(h.clients))
	}()

	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Client message parse error: %v", err)
			continue
		}

		h.mu.RLock()
		tab := msg.Tab
		if tab < 0 || tab >= len(h.tabs) {
			h.mu.RUnlock()
			continue
		}
		entry := h.tabs[tab]
		agent := entry.agent
		h.mu.RUnlock()

		switch msg.Type {
		case "input":
			if entry.Readonly {
				break
			}
			if entry.PtyMgr != nil {
				if err := entry.PtyMgr.Write([]byte(msg.Data)); err != nil {
					log.Printf("PTY write error (tab %d): %v", tab, err)
				}
			} else if agent != nil {
				fwd := mustMarshal(WSMessage{Type: "input", Data: msg.Data, Tab: entry.agentTab})
				safeSend(agent.send, fwd)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if entry.PtyMgr != nil {
					if err := entry.PtyMgr.Resize(msg.Cols, msg.Rows); err != nil {
						log.Printf("PTY resize error (tab %d): %v", tab, err)
					}
				} else if agent != nil {
					fwd := mustMarshal(WSMessage{Type: "resize", Cols: msg.Cols, Rows: msg.Rows, Tab: entry.agentTab})
					safeSend(agent.send, fwd)
				}
			}
		case "restart":
			if entry.NoRestart {
				break
			}
			if entry.PtyMgr != nil {
				outputCh, err := entry.PtyMgr.Restart()
				if err != nil {
					log.Printf("PTY restart error (tab %d): %v", tab, err)
					errMsg := mustMarshal(WSMessage{Type: "error", Data: err.Error(), Tab: tab})
					safeSend(c.send, errMsg)
					break
				}
				h.BroadcastStatus(tab, "restarted")
				h.StartOutputPump(tab, outputCh)
			} else if agent != nil {
				fwd := mustMarshal(WSMessage{Type: "restart", Tab: entry.agentTab})
				safeSend(agent.send, fwd)
			}
		case "close_tab":
			h.mu.Lock()
			if entry.Remote && entry.Status == "disconnected" && !entry.Removed {
				entry.Removed = true
				h.mu.Unlock()
				rmMsg := mustMarshal(WSMessage{Type: "tab_removed", Tab: tab})
				h.broadcastToClients(rmMsg)
				log.Printf("Tab %d closed by client", tab)
			} else {
				h.mu.Unlock()
			}
		}
	}
}

// HandleAttach handles agent WebSocket connections from remote rc instances.
func (h *Hub) HandleAttach(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Agent upgrade error: %v", err)
		return
	}

	// Read registration message (with deadline to prevent hanging connections)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Printf("Agent registration error: %v", err)
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{}) // clear deadline

	var regMsg WSMessage
	if err := json.Unmarshal(raw, &regMsg); err != nil || regMsg.Type != "register" {
		log.Printf("Agent invalid registration")
		conn.Close()
		return
	}

	agentConn := &AgentConn{
		conn:    conn,
		send:    make(chan []byte, channelBufSize),
		numTabs: len(regMsg.Tabs),
	}

	// Create tab entries for agent's commands
	agentAddr := extractIP(r)
	agentUser := regMsg.Data // agent sends "user" in Data field
	agentHostname := ""
	agentWorkspace := ""
	// Parse hostname and workspace from Tabs metadata (first tab carries it)
	if len(regMsg.Tabs) > 0 {
		agentHostname = regMsg.Tabs[0].Hostname
		agentWorkspace = regMsg.Tabs[0].Workspace
	}
	// If the connection comes from loopback, use the agent's hostname instead
	if ip := net.ParseIP(agentAddr); ip != nil && ip.IsLoopback() && agentHostname != "" {
		agentAddr = agentHostname
	}

	h.mu.Lock()
	baseTab := len(h.tabs)
	agentConn.baseTab = baseTab
	for i, ti := range regMsg.Tabs {
		h.tabs = append(h.tabs, &TabEntry{
			Name:      ti.Name,
			Command:   ti.Command,
			Remote:    true,
			Buf:       NewOutputBuffer(remoteTabBufBytes),
			Status:    "running",
			agent:     agentConn,
			agentTab:  i,
			User:      agentUser,
			Hostname:  agentHostname,
			Workspace: agentWorkspace,
			Addr:      agentAddr,
			NoRestart: ti.NoRestart,
			Readonly:  ti.Readonly,
			Upload:    ti.Upload,
		})
	}
	h.agents[agentConn] = true
	h.mu.Unlock()

	log.Printf("Agent attached from %s: %d tabs (indices %d-%d)", r.RemoteAddr, len(regMsg.Tabs), baseTab, baseTab+len(regMsg.Tabs)-1)

	// Notify all browser clients of new tabs
	for i, ti := range regMsg.Tabs {
		tabIdx := baseTab + i
		meta := &TabInfo{
			Command:   ti.Command,
			User:      agentUser,
			Hostname:  agentHostname,
			Workspace: agentWorkspace,
			Addr:      agentAddr,
			NoRestart: ti.NoRestart,
			Readonly:  ti.Readonly,
			Upload:    ti.Upload,
		}
		addMsg := mustMarshal(WSMessage{Type: "tab_added", Tab: tabIdx, Data: ti.Name, Remote: true, Meta: meta})
		h.broadcastToClients(addMsg)
	}

	// Agent writer goroutine with ping
	safeGo("agentWriter", func() {
		ticker := time.NewTicker(wsPingPeriod)
		defer func() {
			ticker.Stop()
			conn.Close()
		}()
		for {
			select {
			case msg, ok := <-agentConn.send:
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

	// Cleanup on agent disconnect
	defer func() {
		h.mu.Lock()
		for i := baseTab; i < baseTab+agentConn.numTabs && i < len(h.tabs); i++ {
			h.tabs[i].Status = "disconnected"
			h.tabs[i].agent = nil
		}
		delete(h.agents, agentConn)
		h.mu.Unlock()

		close(agentConn.send)
		conn.Close()

		for i := 0; i < agentConn.numTabs; i++ {
			h.BroadcastStatus(baseTab+i, "disconnected")
		}
		log.Printf("Agent disconnected (tabs %d-%d)", baseTab, baseTab+agentConn.numTabs-1)
	}()

	// Read messages from agent with pong-based keepalive
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Agent message parse error: %v", err)
			continue
		}
		if msg.Tab < 0 || msg.Tab >= agentConn.numTabs {
			continue
		}
		globalTab := baseTab + msg.Tab

		switch msg.Type {
		case "output":
			// Write to hub's buffer for this tab (for browser replay)
			h.mu.RLock()
			if globalTab < len(h.tabs) {
				h.tabs[globalTab].Buf.Write([]byte(msg.Data))
			}
			h.mu.RUnlock()
			// Broadcast to browsers
			outMsg := mustMarshal(WSMessage{Type: "output", Data: msg.Data, Tab: globalTab})
			h.broadcastToClients(outMsg)

		case "status":
			h.BroadcastStatus(globalTab, msg.Data)

		case "error":
			errMsg := mustMarshal(WSMessage{Type: "error", Data: msg.Data, Tab: globalTab})
			h.broadcastToClients(errMsg)

		case "upload_result":
			h.uploadMu.Lock()
			ch, ok := h.pendingUploads[globalTab]
			if ok {
				delete(h.pendingUploads, globalTab)
			}
			h.uploadMu.Unlock()
			if ok {
				status := 200
				if msg.Cols > 0 { // reuse Cols as HTTP status for errors
					status = int(msg.Cols)
				}
				ch <- uploadResult{Status: status, Body: msg.Data}
			}
		}
	}
}
