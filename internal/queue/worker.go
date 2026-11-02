package queue

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type WorkerOptions struct {
	Queues      []string
	Concurrency int
	Poll        time.Duration
	// StuckAfter is how old a running job's heartbeat must be before another
	// worker takes the job back. Default 5m.
	StuckAfter time.Duration
	// Timeout bounds one attempt of a job that did not set its own.
	// Default StuckAfter/2; it must stay below StuckAfter.
	Timeout time.Duration
	// Heartbeat is how often a running job refreshes its claim.
	// Default StuckAfter/4.
	Heartbeat time.Duration
	// ShutdownTimeout is how long running jobs may keep going once the
	// worker is asked to stop. Past it their context is cancelled. Default 30s.
	ShutdownTimeout time.Duration
	Log             zerolog.Logger
}

// storeTimeout bounds every status write. Those writes do not inherit the
// worker's context: a status lost to a shutdown is a job that runs twice.
const storeTimeout = 10 * time.Second

func (opts *WorkerOptions) defaults() error {
	if opts.Concurrency < 1 {
		opts.Concurrency = 4
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	if opts.StuckAfter <= 0 {
		opts.StuckAfter = 5 * time.Minute
	}
	if opts.Timeout <= 0 {
		opts.Timeout = opts.StuckAfter / 2
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = opts.StuckAfter / 4
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 30 * time.Second
	}
	if len(opts.Queues) == 0 {
		opts.Queues = []string{"default"}
	}

	if opts.Timeout >= opts.StuckAfter {
		return fmt.Errorf("%w: timeout %s must be below stuck_after %s", ErrOptions, opts.Timeout, opts.StuckAfter)
	}
	if opts.Heartbeat >= opts.StuckAfter {
		return fmt.Errorf("%w: heartbeat %s must be below stuck_after %s", ErrOptions, opts.Heartbeat, opts.StuckAfter)
	}
	return nil
}

// Work claims and runs jobs until ctx is cancelled, then drains: it stops
// claiming, lets the running jobs finish for up to ShutdownTimeout, cancels
// the ones still running and records their outcome before returning.
//
// A concurrency slot is taken before a job is claimed, never after: a claimed
// job waiting for a slot would age past StuckAfter and be run by someone else.
func (q *Queue) Work(ctx context.Context, opts WorkerOptions) error {
	if q.store == nil {
		return ErrNoStore
	}
	if err := opts.defaults(); err != nil {
		return err
	}

	// Jobs get their own context: the signal that stops the worker must not
	// abort the jobs it already holds. The drain cancels it when time is up.
	jobs, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()

	reclaimed := make(chan struct{})
	go func() {
		defer close(reclaimed)
		q.reclaim(ctx, opts)
	}()

	var running sync.WaitGroup
	slots := make(chan struct{}, opts.Concurrency)

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			continue
		case slots <- struct{}{}:
		}

		entry, err := q.claim(ctx, opts)
		if err != nil || entry == nil {
			<-slots
			if err != nil {
				opts.Log.Error().Err(err).Msg("queue: claiming a job")
			}
			sleep(ctx, opts.Poll)
			continue
		}

		running.Add(1)
		go func() {
			defer running.Done()
			defer func() { <-slots }()
			if err := q.run(jobs, entry, opts); err != nil && !errors.Is(err, ErrLost) {
				opts.Log.Error().Err(err).Str("job", entry.Name).Uint("id", entry.ID).
					Msg("queue: could not record the outcome of a job")
			}
		}()
	}

	<-reclaimed
	return q.drain(&running, cancelJobs, opts)
}

func (q *Queue) claim(ctx context.Context, opts WorkerOptions) (*Entry, error) {
	// Not the worker's context: a claim committed while the caller gave up
	// on it would leave a running row nobody works on.
	claimCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	return q.store.Claim(claimCtx, opts.Queues, time.Now())
}

// drainGrace is how long cancelled jobs get to return and record their state.
var drainGrace = 5 * time.Second

func (q *Queue) drain(running *sync.WaitGroup, cancelJobs context.CancelFunc, opts WorkerOptions) error {
	finished := make(chan struct{})
	go func() {
		running.Wait()
		close(finished)
	}()

	timer := time.NewTimer(opts.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-finished:
		return nil
	case <-timer.C:
	}

	opts.Log.Warn().Dur("shutdown_timeout", opts.ShutdownTimeout).
		Msg("queue: jobs still running at the end of the drain, cancelling them")
	cancelJobs()

	select {
	case <-finished:
		return nil
	case <-time.After(drainGrace):
		// Their rows stay running: once the heartbeat goes stale another
		// worker takes them back.
		return errors.New("queue: some jobs ignored their cancelled context and were abandoned")
	}
}

// run executes one claimed job and records its outcome.
func (q *Queue) run(jobs context.Context, entry *Entry, opts WorkerOptions) error {
	started := time.Now()
	log := opts.Log.With().Str("job", entry.Name).Uint("id", entry.ID).Int("attempt", entry.Attempts).Logger()

	handler, ok := q.handler(entry.Name)
	if !ok {
		q.stats.failed.Add(1)
		log.Error().Msg("queue: no handler")
		return q.persist(jobs, func(ctx context.Context) error {
			return q.store.Fail(ctx, entry, ErrUnknownJob.Error())
		})
	}

	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = opts.Timeout
	}
	runCtx, cancel := context.WithTimeout(jobs, timeout)
	defer cancel()

	lost := q.heartbeat(runCtx, cancel, entry, opts)
	err := protect(func() error { return handler(runCtx, []byte(entry.Payload)) }, log)
	cancel()
	if <-lost {
		q.stats.lost.Add(1)
		log.Error().Err(err).Msg("queue: the claim on this job was lost while it ran; its outcome is discarded")
		return ErrLost
	}

	if err == nil {
		q.stats.processed.Add(1)
		log.Info().Dur("took", time.Since(started)).Msg("queue: done")
		return q.persist(jobs, func(ctx context.Context) error { return q.store.Complete(ctx, entry) })
	}

	if errors.Is(err, context.DeadlineExceeded) && jobs.Err() == nil {
		err = fmt.Errorf("queue: job timed out after %s: %w", timeout, err)
	}
	message := failure(err, entry.Payload)

	switch {
	case jobs.Err() != nil:
		// Interrupted by the shutdown: the job did nothing wrong, give the
		// attempt back and let the next worker start it right away.
		q.stats.interrupted.Add(1)
		log.Warn().Err(err).Msg("queue: interrupted by the shutdown, released")
		return q.persist(jobs, func(ctx context.Context) error {
			return q.store.Release(ctx, entry, time.Now(), message, true)
		})

	case entry.Attempts >= entry.MaxTries:
		q.stats.failed.Add(1)
		log.Error().Err(err).Msg("queue: giving up")
		return q.persist(jobs, func(ctx context.Context) error { return q.store.Fail(ctx, entry, message) })

	default:
		wait := backoff(entry.Attempts)
		q.stats.retried.Add(1)
		log.Warn().Err(err).Dur("retry_in", wait).Msg("queue: retrying")
		return q.persist(jobs, func(ctx context.Context) error {
			return q.store.Release(ctx, entry, time.Now().Add(wait), message, false)
		})
	}
}

// persist writes a job's status with a context that survives the shutdown,
// retrying a transient failure (a busy SQLite file, a dropped connection).
func (q *Queue) persist(parent context.Context, write func(ctx context.Context) error) error {
	var err error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), storeTimeout)
		err = write(ctx)
		cancel()
		if err == nil || errors.Is(err, ErrLost) {
			break
		}
	}
	switch {
	case errors.Is(err, ErrLost):
		q.stats.lost.Add(1)
	case err != nil:
		q.stats.storeErrors.Add(1)
	}
	return err
}

// heartbeat refreshes the claim until ctx ends. If the claim turns out to be
// lost, it cancels the job. The channel yields whether the claim was lost,
// once the heartbeat has stopped.
func (q *Queue) heartbeat(ctx context.Context, cancel context.CancelFunc, entry *Entry, opts WorkerOptions) <-chan bool {
	lost := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(opts.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				lost <- false
				return
			case <-ticker.C:
			}

			beat, done := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
			err := q.store.Heartbeat(beat, entry, time.Now())
			done()
			if errors.Is(err, ErrLost) {
				cancel()
				lost <- true
				return
			}
			if err != nil {
				opts.Log.Warn().Err(err).Str("job", entry.Name).Uint("id", entry.ID).Msg("queue: heartbeat failed")
			}
		}
	}()
	return lost
}

func (q *Queue) reclaim(ctx context.Context, opts WorkerOptions) {
	interval := opts.StuckAfter / 2
	for {
		requeued, failed, err := q.store.Reclaim(ctx, time.Now().Add(-opts.StuckAfter))
		switch {
		case err != nil && ctx.Err() == nil:
			opts.Log.Error().Err(err).Msg("queue: reclaiming jobs from dead workers")
		case requeued+failed > 0:
			q.stats.reclaimed.Add(requeued + failed)
			q.stats.failed.Add(failed)
			opts.Log.Warn().Int64("requeued", requeued).Int64("failed", failed).
				Msg("queue: reclaimed jobs from a dead worker")
		}
		if !sleep(ctx, interval) {
			return
		}
	}
}

func protect(run func() error, log zerolog.Logger) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error().Str("stack", string(debug.Stack())).Msgf("queue: job panicked: %v", recovered)
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
