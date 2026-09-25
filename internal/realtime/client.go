package realtime

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait       = 10 * time.Second
	pongWait        = 60 * time.Second
	pingPeriod      = pongWait * 9 / 10
	clientQueueSize = 64
	maxMessageSize  = 1024
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
}

func (c *Client) readPump() {
	defer c.hub.Unregister(c)
	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, reader, err := c.conn.NextReader()
		if err != nil {
			return
		}
		// Client messages never drive game state. Drain them with a size limit
		// while continuing to process WebSocket close, ping and pong frames.
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.conn.Close()
	defer c.hub.Unregister(c)
	for {
		// Prioritize disconnects over any queued events.
		select {
		case <-c.done:
			return
		default:
		}
		select {
		case <-c.done:
			return
		case data := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}
