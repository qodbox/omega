package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrUnknownJob = errors.New("queue: no handler registered for this job")
	ErrNoStore    = errors.New("queue: no store configured")
)

type Handler func(ctx context.Context, payload []byte) error

type Options struct {
	Queue    string
	Delay    time.Duration
	MaxTries int
}

type Store interface {
	Push(ctx context.Context, name, queue, payload string, runAt time.Time, maxTries int) error
	Claim(ctx context.Context, queues []string, now time.Time) (*Entry, error)
	Release(ctx context.Context, id uint, runAt time.Time, failure string) error
	Fail(ctx context.Context, id uint, failure string) error
	Complete(ctx context.Context, id uint) error
	Failed(ctx context.Context, limit int) ([]Entry, error)
	Retry(ctx context.Context, id uint) (int64, error)
	Purge(ctx context.Context, status string) (int64, error)
	Pending(ctx context.Context) (int64, error)
	Reclaim(ctx context.Context, olderThan time.Time) (int64, error)
	Sweep(ctx context.Context, status string, olderThan time.Time) (int64, error)
}

type Entry struct {
	ID        uint
	Queue     string
	Name      string
	Payload   string
	Attempts  int
	MaxTries  int
	LastError string
}

type Queue struct {
	mu       sync.RWMutex
	store    Store
	handlers map[string]Handler
	tries    int
}

func New(store Store) *Queue {
	return &Queue{store: store, handlers: map[string]Handler{}, tries: 3}
}

func (q *Queue) Handle(name string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[name] = handler
}

func (q *Queue) Names() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	names := make([]string, 0, len(q.handlers))
	for name := range q.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (q *Queue) handler(name string) (Handler, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	handler, ok := q.handlers[name]
	return handler, ok
}

func (q *Queue) Push(ctx context.Context, name string, payload any, opts ...Options) error {
	if q.store == nil {
		return ErrNoStore
	}
	if _, ok := q.handler(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownJob, name)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	option := Options{Queue: "default", MaxTries: q.tries}
	if len(opts) > 0 {
		if opts[0].Queue != "" {
			option.Queue = opts[0].Queue
		}
		if opts[0].MaxTries > 0 {
			option.MaxTries = opts[0].MaxTries
		}
		option.Delay = opts[0].Delay
	}

	return q.store.Push(ctx, name, option.Queue, string(encoded), time.Now().Add(option.Delay), option.MaxTries)
}

func (q *Queue) Later(ctx context.Context, delay time.Duration, name string, payload any) error {
	return q.Push(ctx, name, payload, Options{Delay: delay})
}

func (q *Queue) Store() Store { return q.store }

func backoff(attempt int) time.Duration {
	seconds := 1 << attempt
	if seconds > 600 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}
