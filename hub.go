package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
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

// TabEntry represents a terminal session, either local or remote (agent-backed).
type TabEntry struct {
	Name      string
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

// Hub manages browser clients, local PTYs, and remote agents.
type Hub struct {
	tabs      []*TabEntry
	clients   map[*Client]bool
	agents    map[*AgentConn]bool
	hostname  string
	workspace string
	user      string
	noRestart bool
	readonly  bool
	mu        sync.RWMutex
}

// Client represents a connected browser WebSocket client.
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// WSMessage is the JSON message format for all WebSocket communication.
type WSMessage struct {
	Type   string    `json:"type"`
	Data   string    `json:"data,omitempty"`
	Cols   uint16    `json:"cols,omitempty"`
	Rows   uint16    `json:"rows,omitempty"`
	Tab    int       `json:"tab"`
	Tabs   []TabInfo `json:"tabs,omitempty"`
	Remote bool      `json:"remote,omitempty"`
	Meta   *TabInfo  `json:"meta,omitempty"`
}

// TabInfo describes a tab in the 'tabs' message.
type TabInfo struct {
	Name      string `json:"name"`
	Remote    bool   `json:"remote,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Pid       int    `json:"pid,omitempty"`
	User      string `json:"user,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Addr      string `json:"addr,omitempty"`
	NoRestart bool   `json:"noRestart,omitempty"`
	Readonly  bool   `json:"readonly,omitempty"`
}

// NewHub creates a new Hub with local PTY sessions.
func NewHub(ptyMgrs []*PTYManager, bufs []*OutputBuffer, tabNames []string, currentUser string, noRestart, readonly bool) *Hub {
	hostname, _ := os.Hostname()
	workspace, _ := os.Getwd()
	h := &Hub{
		tabs:      make([]*TabEntry, len(ptyMgrs)),
		clients:   make(map[*Client]bool),
		agents:    make(map[*AgentConn]bool),
		hostname:  hostname,
		workspace: workspace,
		user:      currentUser,
		noRestart: noRestart,
		readonly:  readonly,
	}
	for i := range ptyMgrs {
		h.tabs[i] = &TabEntry{
			Name:      tabNames[i],
			Buf:       bufs[i],
			Status:    "running",
			PtyMgr:    ptyMgrs[i],
			User:      currentUser,
			Hostname:  hostname,
			Workspace: workspace,
			NoRestart: noRestart,
			Readonly:  readonly,
		}
	}
	return h
}

func (h *Hub) getTabInfos() []TabInfo {
	infos := make([]TabInfo, len(h.tabs))
	for i, t := range h.tabs {
		infos[i] = TabInfo{
			Name:      t.Name,
			Remote:    t.Remote,
			Removed:   t.Removed,
			User:      t.User,
			Hostname:  t.Hostname,
			Workspace: t.Workspace,
			Addr:      t.Addr,
			NoRestart: t.NoRestart,
			Readonly:  t.Readonly,
		}
		if t.PtyMgr != nil {
			infos[i].Pid = t.PtyMgr.Pid()
		}
	}
	return infos
}

// Broadcast sends PTY output to all browser clients (for local tabs).
func (h *Hub) Broadcast(tab int, data []byte) {
	msg, _ := json.Marshal(WSMessage{Type: "output", Data: string(data), Tab: tab})
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

	msg, _ := json.Marshal(WSMessage{Type: "status", Data: status, Tab: tab})
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
	go func() {
		for data := range outputCh {
			h.Broadcast(tab, data)
		}
		h.Broadcast(tab, []byte("\r\n\033[1;31m[rc] Process exited.\033[0m\r\n"))
		h.BroadcastStatus(tab, "exited")
	}()
}

// HandleWebSocket handles browser WebSocket connections.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
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
	tabsMsg, _ := json.Marshal(WSMessage{Type: "tabs", Tabs: tabInfos})
	client.send <- tabsMsg

	// Send history and status for each tab
	for i := 0; i < tabCount; i++ {
		if statuses[i] == "" {
			continue // removed tab
		}
		if len(snapshots[i]) > 0 {
			histMsg, _ := json.Marshal(WSMessage{Type: "output", Data: string(snapshots[i]), Tab: i})
			client.send <- histMsg
		}
		statusMsg, _ := json.Marshal(WSMessage{Type: "status", Data: statuses[i], Tab: i})
		client.send <- statusMsg
	}

	go h.writePump(client)
	go h.readPump(client)
}

func (h *Hub) writePump(c *Client) {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
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

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
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
				fwd, _ := json.Marshal(WSMessage{Type: "input", Data: msg.Data, Tab: entry.agentTab})
				safeSend(agent.send, fwd)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if entry.PtyMgr != nil {
					if err := entry.PtyMgr.Resize(msg.Cols, msg.Rows); err != nil {
						log.Printf("PTY resize error (tab %d): %v", tab, err)
					}
				} else if agent != nil {
					fwd, _ := json.Marshal(WSMessage{Type: "resize", Cols: msg.Cols, Rows: msg.Rows, Tab: entry.agentTab})
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
					errMsg, _ := json.Marshal(WSMessage{Type: "error", Data: err.Error(), Tab: tab})
					safeSend(c.send, errMsg)
					break
				}
				h.BroadcastStatus(tab, "restarted")
				h.StartOutputPump(tab, outputCh)
			} else if agent != nil {
				fwd, _ := json.Marshal(WSMessage{Type: "restart", Tab: entry.agentTab})
				safeSend(agent.send, fwd)
			}
		case "close_tab":
			h.mu.Lock()
			if entry.Remote && entry.Status == "disconnected" && !entry.Removed {
				entry.Removed = true
				h.mu.Unlock()
				rmMsg, _ := json.Marshal(WSMessage{Type: "tab_removed", Tab: tab})
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

	// Read registration message
	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Printf("Agent registration error: %v", err)
		conn.Close()
		return
	}

	var regMsg WSMessage
	if err := json.Unmarshal(raw, &regMsg); err != nil || regMsg.Type != "register" {
		log.Printf("Agent invalid registration")
		conn.Close()
		return
	}

	agentConn := &AgentConn{
		conn:    conn,
		send:    make(chan []byte, 256),
		numTabs: len(regMsg.Tabs),
	}

	// Create tab entries for agent's commands
	agentAddr := strings.Split(r.RemoteAddr, ":")[0]
	agentUser := regMsg.Data // agent sends "user" in Data field
	agentHostname := ""
	agentWorkspace := ""
	// Parse hostname and workspace from Tabs metadata (first tab carries it)
	if len(regMsg.Tabs) > 0 {
		agentHostname = regMsg.Tabs[0].Hostname
		agentWorkspace = regMsg.Tabs[0].Workspace
	}

	h.mu.Lock()
	baseTab := len(h.tabs)
	agentConn.baseTab = baseTab
	for i, ti := range regMsg.Tabs {
		h.tabs = append(h.tabs, &TabEntry{
			Name:      ti.Name,
			Remote:    true,
			Buf:       NewOutputBuffer(10 * 1024 * 1024),
			Status:    "running",
			agent:     agentConn,
			agentTab:  i,
			User:      agentUser,
			Hostname:  agentHostname,
			Workspace: agentWorkspace,
			Addr:      agentAddr,
			NoRestart: ti.NoRestart,
			Readonly:  ti.Readonly,
		})
	}
	h.agents[agentConn] = true
	h.mu.Unlock()

	log.Printf("Agent attached from %s: %d tabs (indices %d-%d)", r.RemoteAddr, len(regMsg.Tabs), baseTab, baseTab+len(regMsg.Tabs)-1)

	// Notify all browser clients of new tabs
	for i, ti := range regMsg.Tabs {
		tabIdx := baseTab + i
		meta := &TabInfo{
			User:      agentUser,
			Hostname:  agentHostname,
			Workspace: agentWorkspace,
			Addr:      agentAddr,
			NoRestart: ti.NoRestart,
			Readonly:  ti.Readonly,
		}
		addMsg, _ := json.Marshal(WSMessage{Type: "tab_added", Tab: tabIdx, Data: ti.Name, Remote: true, Meta: meta})
		h.broadcastToClients(addMsg)
	}

	// Agent writer goroutine
	go func() {
		defer conn.Close()
		for msg := range agentConn.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

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

	// Read messages from agent (output, status, error)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		// Map agent-relative tab index to global tab index
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
			outMsg, _ := json.Marshal(WSMessage{Type: "output", Data: msg.Data, Tab: globalTab})
			h.broadcastToClients(outMsg)

		case "status":
			h.BroadcastStatus(globalTab, msg.Data)

		case "error":
			errMsg, _ := json.Marshal(WSMessage{Type: "error", Data: msg.Data, Tab: globalTab})
			h.broadcastToClients(errMsg)
		}
	}
}
