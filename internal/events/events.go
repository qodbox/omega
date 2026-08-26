package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Listener func(ctx context.Context, payload any) error

type Bus struct {
	mu        sync.RWMutex
	listeners map[string][]named
	log       zerolog.Logger
	pending   sync.WaitGroup
}

type named struct {
	name   string
	listen Listener
	async  bool
}

func New(log zerolog.Logger) *Bus {
	return &Bus{listeners: map[string][]named{}, log: log}
}

func (b *Bus) Listen(event, name string, listener Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[event] = append(b.listeners[event], named{name: name, listen: listener})
}

func (b *Bus) ListenAsync(event, name string, listener Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[event] = append(b.listeners[event], named{name: name, listen: listener, async: true})
}

func (b *Bus) Events() map[string][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := map[string][]string{}
	for event, listeners := range b.listeners {
		for _, listener := range listeners {
			out[event] = append(out[event], listener.name)
		}
	}
	return out
}

func (b *Bus) Emit(ctx context.Context, event string, payload any) error {
	b.mu.RLock()
	listeners := append([]named(nil), b.listeners[event]...)
	b.mu.RUnlock()

	for _, listener := range listeners {
		if listener.async {
			b.pending.Add(1)
			go func(listener named) {
				defer b.pending.Done()
				b.call(context.WithoutCancel(ctx), event, listener, payload)
			}(listener)
			continue
		}
		if err := b.call(ctx, event, listener, payload); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bus) call(ctx context.Context, event string, listener named, payload any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
		if err != nil {
			b.log.Error().Err(err).
				Str("event", event).
				Str("listener", listener.name).
				Msg("events: listener failed")
		}
	}()
	return listener.listen(ctx, payload)
}

func (b *Bus) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		b.pending.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
