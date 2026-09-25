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
