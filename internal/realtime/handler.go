package realtime

import (
	"net/http"

	"github.com/gorilla/websocket"
)

type Handler struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{hub: hub, upgrader: websocket.Upgrader{
		HandshakeTimeout: writeWait,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		CheckOrigin: func(*http.Request) bool {
			// Development only: restrict allowed origins in production.
			return true
		},
	}}
}

// ServeHTTP upgrades a public broadcast-only WebSocket connection.
// @Summary Connect to realtime game events
// @Description WebSocket upgrade required. Events and reconnect snapshot flow are documented in LOCAL_SYSTEM.md.
// @Tags realtime
// @Success 101 {string} string "Switching Protocols"
// @Router /ws [get]
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{hub: h.hub, conn: conn, send: make(chan []byte, clientQueueSize), done: make(chan struct{})}
	if !h.hub.Register(c) {
		conn.Close()
		return
	}
	go c.writePump()
	c.readPump()
}
