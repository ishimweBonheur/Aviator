package realtime

import (
	"encoding/json"
	"sync"
)

// Hub fans out public events without performing network I/O. All membership
// changes and done-channel closes are serialized by mu. Send queues never close.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
	closed  bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]struct{})}
}

func (h *Hub) ClientCount() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.clients) }

func (h *Hub) Register(c *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
	}
	h.clients[c] = struct{}{}
	return true
}

// Unregister is idempotent, including when both pumps exit concurrently.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(c)
}

// remove must be called with mu held. The write pump closes the socket, waking
// the reader, without making the broadcaster wait for network operations.
func (h *Hub) remove(c *Client) {
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.done)
	}
}

func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			h.remove(c)
		}
	}
}

// Close disconnects clients and prevents further registration. No hub goroutine
// is needed, and repeated calls are safe.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		h.remove(c)
	}
}
