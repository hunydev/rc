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
type Hub struct {
	ptyMgr  *PTYManager
	buf     *OutputBuffer
	clients map[*Client]bool
	mu      sync.RWMutex
}

// Client represents a connected WebSocket client.
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// WSMessage is the JSON message format between browser and server.
type WSMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// NewHub creates a new Hub.
func NewHub(ptyMgr *PTYManager, buf *OutputBuffer) *Hub {
	return &Hub{
		ptyMgr:  ptyMgr,
		buf:     buf,
		clients: make(map[*Client]bool),
	}
}

// Broadcast sends data to all connected clients.
func (h *Hub) Broadcast(data []byte) {
	msg, _ := json.Marshal(WSMessage{Type: "output", Data: string(data)})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
		}
	}
}

// BroadcastStatus sends a status message to all connected clients.
func (h *Hub) BroadcastStatus(status string) {
	msg, _ := json.Marshal(WSMessage{Type: "status", Data: status})
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
// When the PTY process exits, it broadcasts the exit status.
func (h *Hub) StartOutputPump(outputCh <-chan []byte) {
	go func() {
		for data := range outputCh {
			h.Broadcast(data)
		}
		h.Broadcast([]byte("\r\n\033[1;31m[rc] Process exited.\033[0m\r\n"))
		h.BroadcastStatus("exited")
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

	// Send session history
	history := h.buf.Snapshot()
	if len(history) > 0 {
		histMsg, _ := json.Marshal(WSMessage{Type: "output", Data: string(history)})
		client.send <- histMsg
	}

	// Send process status
	statusMsg, _ := json.Marshal(WSMessage{Type: "status", Data: func() string {
		if h.ptyMgr.IsRunning() {
			return "running"
		}
		return "exited"
	}()})
	client.send <- statusMsg

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

		switch msg.Type {
		case "input":
			if err := h.ptyMgr.Write([]byte(msg.Data)); err != nil {
				log.Printf("PTY write error: %v", err)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if err := h.ptyMgr.Resize(msg.Cols, msg.Rows); err != nil {
					log.Printf("PTY resize error: %v", err)
				}
			}
		case "restart":
			outputCh, err := h.ptyMgr.Restart()
			if err != nil {
				log.Printf("PTY restart error: %v", err)
				errMsg, _ := json.Marshal(WSMessage{Type: "error", Data: err.Error()})
				c.send <- errMsg
				break
			}
			h.BroadcastStatus("restarted")
			h.StartOutputPump(outputCh)
		}
	}
}
