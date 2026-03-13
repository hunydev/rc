package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub manages WebSocket clients and broadcasts PTY output to all connected clients.
// Supports multiple tabs, each with its own PTYManager and OutputBuffer.
type Hub struct {
	ptyMgrs  []*PTYManager
	bufs     []*OutputBuffer
	tabNames []string
	clients  map[*Client]bool
	mu       sync.RWMutex
}

// Client represents a connected WebSocket client.
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// WSMessage is the JSON message format between browser and server.
type WSMessage struct {
	Type string   `json:"type"`
	Data string   `json:"data,omitempty"`
	Cols uint16   `json:"cols,omitempty"`
	Rows uint16   `json:"rows,omitempty"`
	Tab  int      `json:"tab"`
	Tabs []string `json:"tabs,omitempty"`
}

// NewHub creates a new Hub with multiple PTY sessions.
func NewHub(ptyMgrs []*PTYManager, bufs []*OutputBuffer, tabNames []string) *Hub {
	return &Hub{
		ptyMgrs:  ptyMgrs,
		bufs:     bufs,
		tabNames: tabNames,
		clients:  make(map[*Client]bool),
	}
}

// Broadcast sends data to all connected clients for a specific tab.
func (h *Hub) Broadcast(tab int, data []byte) {
	msg, _ := json.Marshal(WSMessage{Type: "output", Data: string(data), Tab: tab})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
		}
	}
}

// BroadcastStatus sends a status message to all connected clients for a specific tab.
func (h *Hub) BroadcastStatus(tab int, status string) {
	msg, _ := json.Marshal(WSMessage{Type: "status", Data: status, Tab: tab})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
		}
	}
}

// StartOutputPump reads from the PTY output channel and broadcasts to clients.
func (h *Hub) StartOutputPump(tab int, outputCh <-chan []byte) {
	go func() {
		for data := range outputCh {
			h.Broadcast(tab, data)
		}
		h.Broadcast(tab, []byte("\r\n\033[1;31m[rc] Process exited.\033[0m\r\n"))
		h.BroadcastStatus(tab, "exited")
	}()
}

// HandleWebSocket handles new WebSocket connections.
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

	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()

	log.Printf("Client connected: %s (total: %d)", r.RemoteAddr, len(h.clients))

	// Send tab list
	tabsMsg, _ := json.Marshal(WSMessage{Type: "tabs", Tabs: h.tabNames})
	client.send <- tabsMsg

	// Send session history and status for each tab
	for i := range h.ptyMgrs {
		history := h.bufs[i].Snapshot()
		if len(history) > 0 {
			histMsg, _ := json.Marshal(WSMessage{Type: "output", Data: string(history), Tab: i})
			client.send <- histMsg
		}
		status := "exited"
		if h.ptyMgrs[i].IsRunning() {
			status = "running"
		}
		statusMsg, _ := json.Marshal(WSMessage{Type: "status", Data: status, Tab: i})
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

		// Validate tab index
		tab := msg.Tab
		if tab < 0 || tab >= len(h.ptyMgrs) {
			tab = 0
		}

		switch msg.Type {
		case "input":
			if err := h.ptyMgrs[tab].Write([]byte(msg.Data)); err != nil {
				log.Printf("PTY write error (tab %d): %v", tab, err)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if err := h.ptyMgrs[tab].Resize(msg.Cols, msg.Rows); err != nil {
					log.Printf("PTY resize error (tab %d): %v", tab, err)
				}
			}
		case "restart":
			outputCh, err := h.ptyMgrs[tab].Restart()
			if err != nil {
				log.Printf("PTY restart error (tab %d): %v", tab, err)
				errMsg, _ := json.Marshal(WSMessage{Type: "error", Data: err.Error(), Tab: tab})
				c.send <- errMsg
				break
			}
			h.BroadcastStatus(tab, "restarted")
			h.StartOutputPump(tab, outputCh)
		}
	}
}
