package cache

import (
	"encoding/json"
	"sync"
	"time"
)

type item struct {
	value   []byte
	expires time.Time
}

type flight struct {
	wg    sync.WaitGroup
	value any
	err   error
}

type Cache struct {
	mu      sync.RWMutex
	items   map[string]item
	janitor *time.Ticker
	stop    chan struct{}

	flightMu sync.Mutex
	flights  map[string]*flight
}

func New() *Cache {
	c := &Cache{items: map[string]item{}, janitor: time.NewTicker(time.Minute), stop: make(chan struct{}), flights: map[string]*flight{}}
	go c.sweep()
	return c
}

func (c *Cache) sweep() {
	for {
		select {
		case <-c.stop:
			c.janitor.Stop()
			return
		case now := <-c.janitor.C:
			c.mu.Lock()
			for key, held := range c.items {
				if !held.expires.IsZero() && now.After(held.expires) {
					delete(c.items, key)
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *Cache) Close() { close(c.stop) }

func (c *Cache) Put(key string, value any, ttl time.Duration) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	held := item{value: encoded}
	if ttl > 0 {
		held.expires = time.Now().Add(ttl)
	}

	c.mu.Lock()
	c.items[key] = held
	c.mu.Unlock()
	return nil
}

func (c *Cache) Forever(key string, value any) error { return c.Put(key, value, 0) }

func (c *Cache) Get(key string, into any) bool {
	c.mu.RLock()
	held, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		return false
	}
	if !held.expires.IsZero() && time.Now().After(held.expires) {
		c.Forget(key)
		return false
	}
	return json.Unmarshal(held.value, into) == nil
}

func (c *Cache) Has(key string) bool {
	c.mu.RLock()
	held, ok := c.items[key]
	c.mu.RUnlock()
	return ok && (held.expires.IsZero() || time.Now().Before(held.expires))
}

func (c *Cache) Forget(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

func (c *Cache) Flush() {
	c.mu.Lock()
	c.items = map[string]item{}
	c.mu.Unlock()
}

func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache) do(key string, build func() (any, error)) (any, error) {
	c.flightMu.Lock()
	if existing, ok := c.flights[key]; ok {
		c.flightMu.Unlock()
		existing.wg.Wait()
		return existing.value, existing.err
	}

	current := &flight{}
	current.wg.Add(1)
	c.flights[key] = current
	c.flightMu.Unlock()

	defer func() {
		c.flightMu.Lock()
		delete(c.flights, key)
		c.flightMu.Unlock()
		current.wg.Done()
	}()

	current.value, current.err = build()
	return current.value, current.err
}

func Remember[T any](c *Cache, key string, ttl time.Duration, build func() (T, error)) (T, error) {
	var cached T
	if c.Get(key, &cached) {
		return cached, nil
	}

	value, err := c.do(key, func() (any, error) {
		var again T
		if c.Get(key, &again) {
			return again, nil
		}

		fresh, err := build()
		if err != nil {
			return fresh, err
		}
		return fresh, c.Put(key, fresh, ttl)
	})

	typed, _ := value.(T)
	return typed, err
}
