// Package queue is a job queue stored in the application database.
//
// Delivery is at least once with fencing: a job is claimed by one worker at a
// time, its row is kept alive by a heartbeat while it runs, and only the worker
// holding the current claim can complete, retry or fail it. A worker that dies
// mid-job loses its claim once the heartbeat goes stale, and the job runs again
// (that run counts as an attempt). Handlers must therefore be idempotent.
//
// Payloads are stored in clear in the jobs table. Never put a secret in one:
// pass an identifier and load the sensitive data inside the handler.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var (
	ErrUnknownJob = errors.New("queue: no handler registered for this job")
	ErrNoStore    = errors.New("queue: no store configured")
	// ErrLost means the claim on a job is gone: another worker reclaimed it,
	// or an operator removed it. The result of this run must be discarded.
	ErrLost = errors.New("queue: the job is no longer claimed by this worker")
	// ErrOptions reports worker options that would make jobs run twice.
	ErrOptions = errors.New("queue: invalid worker options")
)

// MaxErrorLength bounds last_error: a handler can return an error that quotes
// a whole HTTP response, and the jobs table is not a log store.
const MaxErrorLength = 2048

type Handler func(ctx context.Context, payload []byte) error

type Options struct {
	Queue    string
	Delay    time.Duration
	MaxTries int
	// Timeout bounds one attempt. Zero uses the worker's default
	// (queue.timeout). The handler's context is cancelled when it expires.
	Timeout time.Duration
	// UniqueKey makes the dispatch idempotent: while a job with the same key
	// is pending or running, dispatching another one is a no-op. The key is
	// released when the job completes or fails for good.
	UniqueKey string
}

// NewJob is what a Store persists on Push.
type NewJob struct {
	Name      string
	Queue     string
	Payload   string
	RunAt     time.Time
	MaxTries  int
	Timeout   time.Duration
	UniqueKey string
}

type Store interface {
	// Push stores a job. It reports false, without error, when a job with the
	// same unique key is already pending or running.
	Push(ctx context.Context, job NewJob) (bool, error)
	// Claim takes the oldest due job of the given queues, or returns nil.
	// Two concurrent callers never receive the same job.
	Claim(ctx context.Context, queues []string, now time.Time) (*Entry, error)
	// Heartbeat proves the claim is alive. ErrLost when it is not.
	Heartbeat(ctx context.Context, entry *Entry, now time.Time) error
	// Release puts a claimed job back to pending. refund gives the attempt
	// back (the run was interrupted by a shutdown, not by the job).
	Release(ctx context.Context, entry *Entry, runAt time.Time, failure string, refund bool) error
	Fail(ctx context.Context, entry *Entry, failure string) error
	Complete(ctx context.Context, entry *Entry) error
	Failed(ctx context.Context, limit int) ([]Entry, error)
	Retry(ctx context.Context, id uint) (int64, error)
	Purge(ctx context.Context, status string) (int64, error)
	Pending(ctx context.Context) (int64, error)
	// Counts returns the number of jobs per status.
	Counts(ctx context.Context) (map[string]int64, error)
	// Reclaim handles running jobs whose heartbeat is older than olderThan:
	// back to pending, or failed when they have no attempt left.
	Reclaim(ctx context.Context, olderThan time.Time) (requeued, failed int64, err error)
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
	Timeout   time.Duration
	UniqueKey string
}

// Stats are the counters of this process since it started.
type Stats struct {
	Processed   int64 // completed successfully
	Failed      int64 // gave up: no attempt left, or no handler
	Retried     int64 // failed and scheduled again with a backoff
	Interrupted int64 // put back untouched by a shutdown
	Reclaimed   int64 // taken back from a dead worker
	Lost        int64 // finished after its claim was taken by another worker
	StoreErrors int64 // a status could not be written
}

type counters struct {
	processed, failed, retried, interrupted, reclaimed, lost, storeErrors atomic.Int64
}

type Queue struct {
	mu       sync.RWMutex
	store    Store
	handlers map[string]Handler
	tries    int
	stats    counters
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

// Push enqueues a job. A duplicate of a pending or running unique job is
// silently skipped; use Enqueue to know whether it was.
func (q *Queue) Push(ctx context.Context, name string, payload any, opts ...Options) error {
	_, err := q.Enqueue(ctx, name, payload, opts...)
	return err
}

// Enqueue is Push that reports whether a row was written: false means a job
// with the same UniqueKey was already pending or running.
func (q *Queue) Enqueue(ctx context.Context, name string, payload any, opts ...Options) (bool, error) {
	if q.store == nil {
		return false, ErrNoStore
	}
	if _, ok := q.handler(name); !ok {
		return false, fmt.Errorf("%w: %s", ErrUnknownJob, name)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	option := Options{Queue: "default", MaxTries: q.tries}
	if len(opts) > 0 {
		if opts[0].Queue != "" {
			option.Queue = opts[0].Queue
		}
		if opts[0].MaxTries > 0 {
			option.MaxTries = opts[0].MaxTries
		}
		if opts[0].Timeout > 0 {
			option.Timeout = opts[0].Timeout
		}
		option.Delay = opts[0].Delay
		option.UniqueKey = strings.TrimSpace(opts[0].UniqueKey)
	}

	return q.store.Push(ctx, NewJob{
		Name:      name,
		Queue:     option.Queue,
		Payload:   string(encoded),
		RunAt:     time.Now().Add(option.Delay),
		MaxTries:  option.MaxTries,
		Timeout:   option.Timeout,
		UniqueKey: option.UniqueKey,
	})
}

func (q *Queue) Later(ctx context.Context, delay time.Duration, name string, payload any) error {
	return q.Push(ctx, name, payload, Options{Delay: delay})
}

func (q *Queue) Store() Store { return q.store }

func (q *Queue) Stats() Stats {
	return Stats{
		Processed:   q.stats.processed.Load(),
		Failed:      q.stats.failed.Load(),
		Retried:     q.stats.retried.Load(),
		Interrupted: q.stats.interrupted.Load(),
		Reclaimed:   q.stats.reclaimed.Load(),
		Lost:        q.stats.lost.Load(),
		StoreErrors: q.stats.storeErrors.Load(),
	}
}

const (
	backoffBase = time.Second
	backoffCap  = 10 * time.Minute
)

// backoff is the wait before attempt+1: 2^attempt seconds, capped at ten
// minutes, minus up to 20% of jitter so that jobs failing together (a provider
// outage) do not all come back in the same second.
func backoff(attempt int) time.Duration {
	wait := backoffCap
	if attempt >= 0 && attempt < 20 {
		wait = min(backoffBase<<attempt, backoffCap)
	}
	return wait - time.Duration(rand.Int64N(int64(wait/5)+1))
}

// failure turns a handler error into what is stored in last_error: never the
// payload, never more than MaxErrorLength bytes.
func failure(err error, payload string) string {
	message := err.Error()
	if len(payload) >= 8 && payload != "null" && payload != "{}" {
		message = strings.ReplaceAll(message, payload, "[payload]")
	}
	return truncate(message, MaxErrorLength)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const marker = "… (truncated)"
	cut := limit - len(marker)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + marker
}
