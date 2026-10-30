package realtime

import (
	"net"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
)

// Exercise the real Fiber/fasthttp upgrade boundary, beyond room/ticket tests.
func TestWebsocketUpgradeSendsReady(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/ws", Handler())
	done := make(chan error, 1)
	go func() { done <- app.Listener(listener) }()
	t.Cleanup(func() {
		_ = app.ShutdownWithTimeout(3 * time.Second)
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("websocket test server did not stop")
		}
	})
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, response, err := dialer.Dial("ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("websocket handshake: response=%v err=%v", response, err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready struct {
		Event string `json:"event"`
	}
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready.Event != "socket-ready" {
		t.Fatalf("expected socket-ready, got %q", ready.Event)
	}
}
