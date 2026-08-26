package broadcast

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type Inbound struct {
	Action  string          `json:"action"`
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Handler func(conn *Socket, message Inbound) error

type Socket struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	client *subscriber
	hub    *Hub
	closed bool
}

func (s *Socket) Send(event string, data any) error {
	payload, err := json.Marshal(map[string]any{"event": event, "data": data})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.conn.WriteMessage(websocket.TextMessage, payload)
}

func (s *Socket) Subscribe(channel string) {
	s.hub.mu.Lock()
	s.client.channels[channel] = true
	s.client.filtered = true
	s.hub.mu.Unlock()
}

func (s *Socket) Unsubscribe(channel string) {
	s.hub.mu.Lock()
	delete(s.client.channels, channel)
	s.hub.mu.Unlock()
}

func Upgrade() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}
}

func (h *Hub) Socket(handle Handler) fiber.Handler {
	return websocket.New(func(conn *websocket.Conn) {
		client := h.subscribe(nil)
		socket := &Socket{conn: conn, client: client, hub: h}

		defer func() {
			socket.mu.Lock()
			socket.closed = true
			socket.mu.Unlock()
			h.unsubscribe(client)
			_ = conn.Close()
		}()

		go func() {
			for message := range client.inbox {
				if socket.Send(message.Event, message.Data) != nil {
					return
				}
			}
		}()

		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		})

		for {
			kind, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.TextMessage {
				continue
			}

			var message Inbound
			if json.Unmarshal(raw, &message) != nil {
				_ = socket.Send("error", "malformed message")
				continue
			}

			switch message.Action {
			case "subscribe":
				socket.Subscribe(message.Channel)
				_ = socket.Send("subscribed", message.Channel)
			case "unsubscribe":
				socket.Unsubscribe(message.Channel)
				_ = socket.Send("unsubscribed", message.Channel)
			default:
				if handle == nil {
					_ = socket.Send("error", "unknown action")
					continue
				}
				if err := handle(socket, message); err != nil {
					_ = socket.Send("error", err.Error())
				}
			}
		}
	})
}
