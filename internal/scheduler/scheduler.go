package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Task func(ctx context.Context) error

type entry struct {
	name     string
	every    time.Duration
	at       string
	task     Task
	overlap  bool
	running  bool
	lastRun  time.Time
	nextRun  time.Time
	failures int
}

type Scheduler struct {
	mu      sync.Mutex
	entries []*entry
	log     zerolog.Logger
}

func New(log zerolog.Logger) *Scheduler {
	return &Scheduler{log: log}
}

func (s *Scheduler) Every(name string, every time.Duration, task Task) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, &entry{
		name:    name,
		every:   every,
		task:    task,
		nextRun: time.Now().Add(every),
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
		nextRun: nextDaily(time.Now(), clock),
	})
	return s
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

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := make([]*entry, 0, len(s.entries))
	for _, item := range s.entries {
		if now.Before(item.nextRun) || (item.running && !item.overlap) {
			continue
		}
		item.running = true
		item.lastRun = now
		if item.at != "" {
			item.nextRun = nextDaily(now, item.at)
		} else {
			item.nextRun = now.Add(item.every)
		}
		due = append(due, item)
	}
	s.mu.Unlock()

	for _, item := range due {
		go s.launch(ctx, item)
	}
}

func (s *Scheduler) launch(ctx context.Context, item *entry) {
	started := time.Now()
	err := protect(func() error { return item.task(ctx) })

	s.mu.Lock()
	item.running = false
	if err != nil {
		item.failures++
	}
	s.mu.Unlock()

	if err != nil {
		s.log.Error().Err(err).Str("task", item.name).Msg("scheduler: task failed")
		return
	}
	s.log.Info().Str("task", item.name).Dur("took", time.Since(started)).Msg("scheduler: task done")
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

func protect(run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return run()
}
