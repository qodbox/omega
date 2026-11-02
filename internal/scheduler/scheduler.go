// Package scheduler runs periodic and daily tasks.
//
// Every task is due at fixed points in time (Every aligns on multiples of its
// interval, DailyAt on a wall clock time), so every replica of `schedule:run`
// computes the same slots. With a Locker, the replicas race for each slot and
// only one runs it.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Task func(ctx context.Context) error

// Locker makes a task run once per slot across replicas.
type Locker interface {
	// Acquire claims the run of name scheduled at slot, for ttl. exclusive
	// refuses while the lease of an earlier run is still alive (no overlap).
	// It returns false when another replica has the slot.
	Acquire(ctx context.Context, name string, slot time.Time, ttl time.Duration, exclusive bool) (bool, error)
	// Release ends the lease taken for slot.
	Release(ctx context.Context, name string, slot time.Time) error
}

const dailyTimeout = time.Hour

type entry struct {
	name     string
	every    time.Duration
	at       string
	task     Task
	overlap  bool
	timeout  time.Duration
	running  int
	lastRun  time.Time
	nextRun  time.Time
	failures int
}

type Scheduler struct {
	mu       sync.Mutex
	entries  []*entry
	log      zerolog.Logger
	locker   Locker
	shutdown time.Duration
	inflight sync.WaitGroup
}

func New(log zerolog.Logger) *Scheduler {
	return &Scheduler{log: log, shutdown: 30 * time.Second}
}

// UseLocker shares the schedule with the other replicas through locker.
func (s *Scheduler) UseLocker(locker Locker) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locker = locker
	return s
}

// ShutdownTimeout is how long Run waits for running tasks once stopped.
func (s *Scheduler) ShutdownTimeout(d time.Duration) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d > 0 {
		s.shutdown = d
	}
	return s
}

// Every runs task at every multiple of interval (aligned on the clock, so
// Every(time.Hour) runs at the top of each hour).
func (s *Scheduler) Every(name string, every time.Duration, task Task) *Scheduler {
	if every <= 0 {
		every = time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, &entry{
		name:    name,
		every:   every,
		task:    task,
		timeout: every,
		nextRun: nextEvery(time.Now(), every),
	})
	return s
}

func (s *Scheduler) DailyAt(name, clock string, task Task) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, &entry{
		name:    name,
		at:      clock,
		task:    task,
		timeout: dailyTimeout,
		nextRun: nextDaily(time.Now(), clock),
	})
	return s
}

// WithTimeout bounds each run of the task registered last. The default is
// the task's interval (one hour for a daily task).
func (s *Scheduler) WithTimeout(timeout time.Duration) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) > 0 && timeout > 0 {
		s.entries[len(s.entries)-1].timeout = timeout
	}
	return s
}

// AllowOverlap lets the task registered last start while its previous run is
// still going. By default a slot is skipped when the previous run is not done.
func (s *Scheduler) AllowOverlap() *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) > 0 {
		s.entries[len(s.entries)-1].overlap = true
	}
	return s
}

// ErrUnknownTask is returned by RunTask for a name nobody registered.
var ErrUnknownTask = errors.New("scheduler: no task with this name")

// RunTask runs a task once, now, in the caller's goroutine, with its timeout
// and panic recovery but without the replica lock: for a manual run or a test.
func (s *Scheduler) RunTask(ctx context.Context, name string) error {
	s.mu.Lock()
	var found *entry
	for _, item := range s.entries {
		if item.name == name {
			found = item
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return fmt.Errorf("%w: %s", ErrUnknownTask, name)
	}

	runCtx, cancel := context.WithTimeout(ctx, found.timeout)
	defer cancel()
	return protect(func() error { return found.task(runCtx) }, s.log.With().Str("task", name).Logger())
}

// Names lists the registered tasks.
func (s *Scheduler) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.entries))
	for _, item := range s.entries {
		names = append(names, item.name)
	}
	sort.Strings(names)
	return names
}

func (s *Scheduler) Tasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.entries))
	for _, item := range s.entries {
		when := item.every.String()
		if item.at != "" {
			when = "daily at " + item.at
		}
		names = append(names, fmt.Sprintf("%-28s %-16s next %s", item.name, when, item.nextRun.Format("15:04:05")))
	}
	sort.Strings(names)
	return names
}

// Run fires the due tasks every second until ctx is cancelled, then waits up
// to the shutdown timeout for the running ones, cancelling them past it.
func (s *Scheduler) Run(ctx context.Context) error {
	tasks, cancelTasks := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelTasks()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return s.drain(cancelTasks)
		case now := <-ticker.C:
			s.tick(tasks, now)
		}
	}
}

func (s *Scheduler) drain(cancelTasks context.CancelFunc) error {
	finished := make(chan struct{})
	go func() {
		s.inflight.Wait()
		close(finished)
	}()

	s.mu.Lock()
	wait := s.shutdown
	s.mu.Unlock()

	select {
	case <-finished:
		return nil
	case <-time.After(wait):
	}

	cancelTasks()
	select {
	case <-finished:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("scheduler: some tasks ignored their cancelled context")
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	type run struct {
		item *entry
		slot time.Time
	}

	s.mu.Lock()
	due := make([]run, 0, len(s.entries))
	for _, item := range s.entries {
		if now.Before(item.nextRun) {
			continue
		}
		slot := item.nextRun
		if item.at != "" {
			item.nextRun = nextDaily(now, item.at)
		} else {
			item.nextRun = nextEvery(now, item.every)
		}
		if item.running > 0 && !item.overlap {
			s.log.Warn().Str("task", item.name).Msg("scheduler: previous run still going, slot skipped")
			continue
		}
		item.running++
		item.lastRun = now
		due = append(due, run{item, slot})
	}
	s.mu.Unlock()

	for _, run := range due {
		s.inflight.Add(1)
		go s.launch(ctx, run.item, run.slot)
	}
}

func (s *Scheduler) launch(ctx context.Context, item *entry, slot time.Time) {
	defer s.inflight.Done()
	defer func() {
		s.mu.Lock()
		item.running--
		s.mu.Unlock()
	}()

	log := s.log.With().Str("task", item.name).Time("slot", slot).Logger()

	s.mu.Lock()
	locker := s.locker
	s.mu.Unlock()

	if locker != nil {
		lockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		acquired, err := locker.Acquire(lockCtx, item.name, slot, item.timeout, !item.overlap)
		cancel()
		if err != nil {
			// Fail closed: running without the lock is what runs a task twice.
			log.Error().Err(err).Msg("scheduler: could not take the task lock, slot skipped")
			return
		}
		if !acquired {
			log.Debug().Msg("scheduler: slot taken by another replica")
			return
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if err := locker.Release(releaseCtx, item.name, slot); err != nil {
				log.Warn().Err(err).Msg("scheduler: could not release the task lock")
			}
		}()
	}

	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, item.timeout)
	defer cancel()
	err := protect(func() error { return item.task(runCtx) }, log)

	if err != nil {
		s.mu.Lock()
		item.failures++
		s.mu.Unlock()
		log.Error().Err(err).Msg("scheduler: task failed")
		return
	}
	log.Info().Dur("took", time.Since(started)).Msg("scheduler: task done")
}

// nextEvery is the first multiple of every after from. Multiples are counted
// from a fixed origin (Go's zero time, a UTC midnight), so all replicas agree
// on them whatever their start time.
func nextEvery(from time.Time, every time.Duration) time.Time {
	return from.Truncate(every).Add(every)
}

func nextDaily(from time.Time, clock string) time.Time {
	var hour, minute int
	if _, err := fmt.Sscanf(clock, "%d:%d", &hour, &minute); err != nil {
		return from.Add(24 * time.Hour)
	}

	next := time.Date(from.Year(), from.Month(), from.Day(), hour, minute, 0, 0, from.Location())
	if !next.After(from) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func protect(run func() error, log zerolog.Logger) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error().Str("stack", string(debug.Stack())).Msgf("scheduler: task panicked: %v", recovered)
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return run()
}
