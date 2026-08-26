package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type WorkerOptions struct {
	Queues      []string
	Concurrency int
	Poll        time.Duration
	StuckAfter  time.Duration
	Log         zerolog.Logger
}

func (q *Queue) Work(ctx context.Context, opts WorkerOptions) error {
	if q.store == nil {
		return ErrNoStore
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 4
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	if opts.StuckAfter <= 0 {
		opts.StuckAfter = 5 * time.Minute
	}
	if len(opts.Queues) == 0 {
		opts.Queues = []string{"default"}
	}

	go q.reclaim(ctx, opts)

	var waiting sync.WaitGroup
	slots := make(chan struct{}, opts.Concurrency)

	for {
		select {
		case <-ctx.Done():
			waiting.Wait()
			return nil
		default:
		}

		entry, err := q.store.Claim(ctx, opts.Queues, time.Now())
		if err != nil {
			opts.Log.Error().Err(err).Msg("queue: claiming a job")
			if !sleep(ctx, opts.Poll) {
				waiting.Wait()
				return nil
			}
			continue
		}
		if entry == nil {
			if !sleep(ctx, opts.Poll) {
				waiting.Wait()
				return nil
			}
			continue
		}

		slots <- struct{}{}
		waiting.Add(1)
		go func(entry *Entry) {
			defer waiting.Done()
			defer func() { <-slots }()
			q.run(ctx, entry, opts.Log)
		}(entry)
	}
}

func (q *Queue) run(ctx context.Context, entry *Entry, log zerolog.Logger) {
	started := time.Now()

	handler, ok := q.handler(entry.Name)
	if !ok {
		_ = q.store.Fail(ctx, entry.ID, ErrUnknownJob.Error())
		log.Error().Str("job", entry.Name).Uint("id", entry.ID).Msg("queue: no handler")
		return
	}

	err := protect(func() error { return handler(ctx, []byte(entry.Payload)) })

	if err == nil {
		_ = q.store.Complete(ctx, entry.ID)
		log.Info().
			Str("job", entry.Name).
			Uint("id", entry.ID).
			Dur("took", time.Since(started)).
			Msg("queue: done")
		return
	}

	if entry.Attempts >= entry.MaxTries {
		_ = q.store.Fail(ctx, entry.ID, err.Error())
		log.Error().Err(err).
			Str("job", entry.Name).
			Uint("id", entry.ID).
			Int("attempts", entry.Attempts).
			Msg("queue: giving up")
		return
	}

	wait := backoff(entry.Attempts)
	_ = q.store.Release(ctx, entry.ID, time.Now().Add(wait), err.Error())
	log.Warn().Err(err).
		Str("job", entry.Name).
		Uint("id", entry.ID).
		Int("attempt", entry.Attempts).
		Dur("retry_in", wait).
		Msg("queue: retrying")
}

func (q *Queue) reclaim(ctx context.Context, opts WorkerOptions) {
	ticker := time.NewTicker(opts.StuckAfter)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := q.store.Reclaim(ctx, time.Now().Add(-opts.StuckAfter))
			if err == nil && count > 0 {
				opts.Log.Warn().Int64("jobs", count).Msg("queue: reclaimed jobs from a dead worker")
			}
		}
	}
}

func protect(run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return run()
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
