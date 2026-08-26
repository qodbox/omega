package broadcast

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	client "github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
)

type wsClient struct {
	conn *client.Conn
	t    *testing.T
}

func (c *wsClient) send(t *testing.T, payload any) {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.conn.WriteMessage(client.TextMessage, raw); err != nil {
		t.Fatalf("envoi: %v", err)
	}
}

func (c *wsClient) expect(t *testing.T, event string) map[string]any {
	t.Helper()

	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			t.Fatalf("lecture en attendant %q: %v", event, err)
		}

		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("trame illisible %q: %v", raw, err)
		}
		if frame["event"] == event {
			return frame
		}
	}
}

func socketServer(t *testing.T, hub *Hub, handle Handler) *wsClient {
	t.Helper()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/ws", Upgrade(), hub.Socket(handle))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	url := "ws://" + listener.Addr().String() + "/ws"

	var conn *client.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, _, err = client.DefaultDialer.Dial(url, nil)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("connexion websocket impossible: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &wsClient{conn: conn, t: t}
}

var errAction = errors.New("action refusee")

func TestSocketSubscribesThenReceivesOnlyItsChannel(t *testing.T) {
	hub := New()
	c := socketServer(t, hub, nil)

	c.send(t, map[string]any{"action": "subscribe", "channel": "factures"})
	c.expect(t, "subscribed")

	waitForCount(t, hub, 1)
	hub.Publish("factures", "creee", map[string]any{"id": 7})

	frame := c.expect(t, "creee")
	data, _ := frame["data"].(map[string]any)
	if data["id"] != float64(7) {
		t.Fatalf("charge recue = %v", frame["data"])
	}
}

func TestSocketUnsubscribesAndGoesQuiet(t *testing.T) {
	hub := New()
	c := socketServer(t, hub, nil)

	c.send(t, map[string]any{"action": "subscribe", "channel": "factures"})
	c.expect(t, "subscribed")

	c.send(t, map[string]any{"action": "unsubscribe", "channel": "factures"})
	c.expect(t, "unsubscribed")

	if delivered := hub.Publish("factures", "creee", 1); delivered != 0 {
		t.Fatalf("le client desabonne recoit encore: %d", delivered)
	}
	if delivered := hub.Publish("autre", "creee", 1); delivered != 0 {
		t.Fatalf("le client desabonne recoit un autre canal: %d", delivered)
	}
}

func TestSocketReportsMalformedAndUnknownMessages(t *testing.T) {
	hub := New()
	c := socketServer(t, hub, nil)

	if err := c.conn.WriteMessage(client.TextMessage, []byte("pas du json")); err != nil {
		t.Fatal(err)
	}
	if frame := c.expect(t, "error"); frame["data"] != "malformed message" {
		t.Errorf("data = %v", frame["data"])
	}

	c.send(t, map[string]any{"action": "danser"})
	if frame := c.expect(t, "error"); frame["data"] != "unknown action" {
		t.Errorf("data = %v", frame["data"])
	}
}

func TestSocketRoutesUnknownActionsToTheHandler(t *testing.T) {
	hub := New()
	c := socketServer(t, hub, func(socket *Socket, message Inbound) error {
		if message.Action == "ping" {
			return socket.Send("pong", message.Channel)
		}
		return errAction
	})

	c.send(t, map[string]any{"action": "ping", "channel": "salon"})
	if frame := c.expect(t, "pong"); frame["data"] != "salon" {
		t.Errorf("data = %v", frame["data"])
	}

	c.send(t, map[string]any{"action": "autre"})
	if frame := c.expect(t, "error"); frame["data"] != errAction.Error() {
		t.Errorf("data = %v", frame["data"])
	}
}

func TestSocketReleasesItsSubscriberOnDisconnect(t *testing.T) {
	hub := New()
	c := socketServer(t, hub, nil)

	c.send(t, map[string]any{"action": "subscribe", "channel": "x"})
	c.expect(t, "subscribed")
	waitForCount(t, hub, 1)

	_ = c.conn.Close()
	waitForCount(t, hub, 0)
}

func waitForCount(t *testing.T, hub *Hub, want int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Count() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("abonnes = %d, want %d", hub.Count(), want)
}
