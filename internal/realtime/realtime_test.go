package realtime

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSlowClientDoesNotBlockBroadcast(t *testing.T) {
	h := NewHub()
	defer h.Close()
	slow := &Client{send: make(chan []byte, 1), done: make(chan struct{})}
	fast := &Client{send: make(chan []byte, 3), done: make(chan struct{})}
	h.Register(slow)
	h.Register(fast)
	for range 3 {
		h.Broadcast(Event{Type: EventRoundOpened, RoundID: 147, RoundNumber: 147})
	}
	select {
	case <-slow.done:
	default:
		t.Fatal("slow client was not removed")
	}
	if len(fast.send) != 3 {
		t.Fatal("healthy client missed events")
	}
	h.Unregister(slow)
	if h.Register(slow) {
		t.Fatal("disconnected client registered again")
	}
}

func TestConcurrentHubLifecycle(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			c := &Client{send: make(chan []byte, 4), done: make(chan struct{})}
			if !h.Register(c) {
				return
			}
			for range 100 {
				h.Broadcast(Event{Type: EventRoundStarted})
			}
			h.Unregister(c)
			h.Unregister(c)
		})
	}
	wg.Go(h.Close)
	wg.Wait()
	h.Close()
	if len(h.clients) != 0 {
		t.Fatal("clients remain after close")
	}
}

func TestWebSocketDeliveryAndDisconnect(t *testing.T) {
	h := NewHub()
	server := httptest.NewServer(NewHandler(h))
	defer server.Close()
	defer h.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// The upgrade response can arrive before hub registration completes.
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.clients)
		h.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client did not register")
		}
		time.Sleep(time.Millisecond)
	}
	want := Event{Type: EventMultiplierUpdate, RoundID: 147, RoundNumber: 147, Multiplier: "2.30"}
	h.Broadcast(want)
	conn.SetReadDeadline(deadline)
	var got Event
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), deadline); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("expected normal close reply, got %v", err)
	}
}

func TestHandlerRejectsNonGET(t *testing.T) {
	h := NewHub()
	defer h.Close()
	w := httptest.NewRecorder()
	NewHandler(h).ServeHTTP(w, httptest.NewRequest("POST", "/ws", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatalf("unexpected response: %v", w.Result())
	}
}
