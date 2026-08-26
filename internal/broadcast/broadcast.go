package broadcast

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type Message struct {
	Event string
	Data  any
}

type subscriber struct {
	channels map[string]bool
	filtered bool
	inbox    chan Message
	once     sync.Once
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]bool
	buffer      int
	dropped     atomic.Uint64
}

func New() *Hub {
	return &Hub{subscribers: map[*subscriber]bool{}, buffer: 16}
}

func (h *Hub) subscribe(channels []string) *subscriber {
	wanted := map[string]bool{}
	for _, channel := range channels {
		wanted[channel] = true
	}

	client := &subscriber{channels: wanted, filtered: len(wanted) > 0, inbox: make(chan Message, h.buffer)}

	h.mu.Lock()
	h.subscribers[client] = true
	h.mu.Unlock()
	return client
}

func (h *Hub) unsubscribe(client *subscriber) {
	h.mu.Lock()
	delete(h.subscribers, client)
	h.mu.Unlock()
	client.once.Do(func() { close(client.inbox) })
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	clients := make([]*subscriber, 0, len(h.subscribers))
	for client := range h.subscribers {
		clients = append(clients, client)
	}
	h.subscribers = map[*subscriber]bool{}
	h.mu.Unlock()

	for _, client := range clients {
		client.once.Do(func() { close(client.inbox) })
	}
}

func (h *Hub) Publish(channel, event string, data any) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	delivered := 0
	for client := range h.subscribers {
		if client.filtered && !client.channels[channel] {
			continue
		}
		select {
		case client.inbox <- Message{Event: event, Data: data}:
			delivered++
		default:
			h.dropped.Add(1)
		}
	}
	return delivered
}

func (h *Hub) Dropped() uint64 { return h.dropped.Load() }

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

func (h *Hub) Stream(heartbeat time.Duration) fiber.Handler {
	if heartbeat <= 0 {
		heartbeat = 25 * time.Second
	}

	return func(c *fiber.Ctx) error {
		channels := c.Query("channels")
		wanted := splitAndTrim(channels)

		c.Set(fiber.HeaderContentType, "text/event-stream")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderConnection, "keep-alive")
		c.Set("X-Accel-Buffering", "no")

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			client := h.subscribe(wanted)
			defer h.unsubscribe(client)

			ticker := time.NewTicker(heartbeat)
			defer ticker.Stop()

			if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}

			for {
				select {
				case message, open := <-client.inbox:
					if !open {
						return
					}
					payload, err := json.Marshal(message.Data)
					if err != nil {
						continue
					}
					if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", message.Event, payload); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-ticker.C:
					if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		}))

		return nil
	}
}

func splitAndTrim(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
