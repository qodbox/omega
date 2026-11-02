package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/app/models"
)

var quiet = zerolog.Nop()

// startWorker runs q.Work in the background; stop cancels it and returns
// what Work returned.
func startWorker(t *testing.T, q *Queue, opts WorkerOptions) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- q.Work(ctx, opts) }()

	var once sync.Once
	var result error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case result = <-done:
			case <-time.After(30 * time.Second):
				result = errors.New("the worker did not stop")
			}
		})
		return result
	}
	t.Cleanup(func() { _ = stop() })
	return stop
}

func eventually(t *testing.T, within time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func jobRows(t *testing.T, db *gorm.DB) []models.Job {
	t.Helper()
	var rows []models.Job
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read jobs: %v", err)
	}
	return rows
}

func fast(opts WorkerOptions) WorkerOptions {
	if opts.Poll == 0 {
		opts.Poll = 5 * time.Millisecond
	}
	if opts.StuckAfter == 0 {
		opts.StuckAfter = time.Minute
	}
	opts.Log = quiet
	return opts
}

// N workers (each its own Queue, as separate processes would be), M jobs:
// every job runs exactly once.
func TestEveryJobRunsExactlyOnceAcrossWorkers(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		const workers, jobs = 4, 120

		var mu sync.Mutex
		runs := map[int]int{}
		handler := func(ctx context.Context, payload []byte) error {
			var n int
			if _, err := fmt.Sscanf(string(payload), `{"n":%d}`, &n); err != nil {
				return err
			}
			mu.Lock()
			runs[n]++
			mu.Unlock()
			time.Sleep(time.Millisecond)
			return nil
		}

		producer := New(NewStore(db))
		producer.Handle("count", handler)
		for i := range jobs {
			if err := producer.Push(context.Background(), "count", map[string]int{"n": i}); err != nil {
				t.Fatalf("push: %v", err)
			}
		}

		queues := make([]*Queue, workers)
		stops := make([]func() error, workers)
		for i := range workers {
			queues[i] = New(NewStore(db))
			queues[i].Handle("count", handler)
			stops[i] = startWorker(t, queues[i], fast(WorkerOptions{Concurrency: 4}))
		}

		eventually(t, 60*time.Second, "every job to complete", func() bool {
			var left int64
			db.Model(&models.Job{}).Count(&left)
			return left == 0
		})
		for _, stop := range stops {
			if err := stop(); err != nil {
				t.Errorf("stop: %v", err)
			}
		}

		mu.Lock()
		defer mu.Unlock()
		if len(runs) != jobs {
			t.Errorf("%d distinct jobs ran, want %d", len(runs), jobs)
		}
		for n, count := range runs {
			if count != 1 {
				t.Errorf("job %d ran %d times", n, count)
			}
		}

		var processed int64
		for _, q := range queues {
			processed += q.Stats().Processed
		}
		if processed != jobs {
			t.Errorf("processed counters sum to %d, want %d", processed, jobs)
		}
	})
}

// Problem a: a claimed job must never wait for a slot.
func TestAJobIsClaimedOnlyOnceASlotIsFree(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		release := make(chan struct{})
		started := make(chan struct{}, 2)
		q.Handle("block", func(ctx context.Context, _ []byte) error {
			started <- struct{}{}
			<-release
			return nil
		})

		for range 2 {
			if err := q.Push(context.Background(), "block", nil); err != nil {
				t.Fatal(err)
			}
		}

		stop := startWorker(t, q, fast(WorkerOptions{Concurrency: 1}))
		<-started
		time.Sleep(50 * time.Millisecond)

		var running, pending int64
		db.Model(&models.Job{}).Where("status = ?", models.JobRunning).Count(&running)
		db.Model(&models.Job{}).Where("status = ?", models.JobPending).Count(&pending)
		if running != 1 || pending != 1 {
			t.Errorf("running=%d pending=%d, want 1 and 1: the second job was claimed without a slot", running, pending)
		}

		close(release)
		eventually(t, 10*time.Second, "both jobs to finish", func() bool { return len(jobRows(t, db)) == 0 })
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	})
}

// Problem c: stopping the worker lets the running job finish with a live
// context, and records its completion.
func TestShutdownLetsTheRunningJobFinish(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		started := make(chan struct{})
		var sawCancel atomic.Bool
		q.Handle("slow", func(ctx context.Context, _ []byte) error {
			close(started)
			select {
			case <-ctx.Done():
				sawCancel.Store(true)
				return ctx.Err()
			case <-time.After(150 * time.Millisecond):
				return nil
			}
		})
		if err := q.Push(context.Background(), "slow", nil); err != nil {
			t.Fatal(err)
		}

		stop := startWorker(t, q, fast(WorkerOptions{ShutdownTimeout: 5 * time.Second}))
		<-started
		if err := stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}

		if sawCancel.Load() {
			t.Error("the job saw its context cancelled by the shutdown")
		}
		if rows := jobRows(t, db); len(rows) != 0 {
			t.Errorf("the job is still there after a clean drain: %+v", rows[0])
		}
		if q.Stats().Processed != 1 {
			t.Errorf("processed = %d, want 1", q.Stats().Processed)
		}
	})
}

// Problem c: past the drain timeout the job is cancelled and put back pending
// with its attempt refunded, not left running.
func TestShutdownPastTheDrainTimeoutReleasesTheJob(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		started := make(chan struct{})
		q.Handle("stubborn", func(ctx context.Context, _ []byte) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		if err := q.Push(context.Background(), "stubborn", nil, Options{MaxTries: 1}); err != nil {
			t.Fatal(err)
		}

		stop := startWorker(t, q, fast(WorkerOptions{ShutdownTimeout: 50 * time.Millisecond}))
		<-started
		if err := stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}

		rows := jobRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		job := rows[0]
		if job.Status != models.JobPending || job.Attempts != 0 || job.ClaimedAt != nil {
			t.Errorf("job = status %s attempts %d claimed %v, want pending, 0, nil", job.Status, job.Attempts, job.ClaimedAt)
		}
		if q.Stats().Interrupted != 1 {
			t.Errorf("interrupted = %d, want 1", q.Stats().Interrupted)
		}
	})
}

// Problem d: a job gets a deadline, and a timed out attempt is retried.
func TestAJobIsCancelledAtItsTimeout(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		q.Handle("hang", func(ctx context.Context, _ []byte) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if err := q.Push(context.Background(), "hang", nil, Options{Timeout: 30 * time.Millisecond, MaxTries: 5}); err != nil {
			t.Fatal(err)
		}

		stop := startWorker(t, q, fast(WorkerOptions{}))
		eventually(t, 10*time.Second, "the attempt to time out", func() bool { return q.Stats().Retried >= 1 })
		_ = stop()

		job := jobRows(t, db)[0]
		if job.Status != models.JobPending || job.Attempts != 1 {
			t.Errorf("job = %s/%d, want pending after 1 attempt", job.Status, job.Attempts)
		}
		if !strings.Contains(job.LastError, "timed out after 30ms") {
			t.Errorf("last_error = %q, want the timeout", job.LastError)
		}
	})
}

// Problem d: a job longer than StuckAfter keeps its claim through the
// heartbeat, so the other worker's reclaim never runs it a second time.
func TestAHeartbeatKeepsALongJobFromBeingReclaimed(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		var runs atomic.Int32
		handler := func(ctx context.Context, _ []byte) error {
			runs.Add(1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(900 * time.Millisecond):
				return nil
			}
		}

		opts := fast(WorkerOptions{
			StuckAfter: 300 * time.Millisecond,
			Heartbeat:  50 * time.Millisecond,
			Timeout:    100 * time.Millisecond,
		})

		first, second := New(NewStore(db)), New(NewStore(db))
		first.Handle("long", handler)
		second.Handle("long", handler)
		if err := first.Push(context.Background(), "long", nil, Options{Timeout: 5 * time.Second}); err != nil {
			t.Fatal(err)
		}

		stopFirst := startWorker(t, first, opts)
		eventually(t, 5*time.Second, "the first run", func() bool { return runs.Load() == 1 })
		stopSecond := startWorker(t, second, opts)

		eventually(t, 10*time.Second, "the job to complete", func() bool { return len(jobRows(t, db)) == 0 })
		_ = stopFirst()
		_ = stopSecond()

		if got := runs.Load(); got != 1 {
			t.Errorf("the long job ran %d times, want 1", got)
		}
		if reclaimed := first.Stats().Reclaimed + second.Stats().Reclaimed; reclaimed != 0 {
			t.Errorf("reclaimed = %d, want 0", reclaimed)
		}
	})
}

// Problem d: a worker whose claim was taken away discards its outcome
// instead of overwriting the new claimant's row.
func TestALostClaimCancelsTheJobAndKeepsTheRowUntouched(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		started := make(chan struct{})
		cancelled := make(chan struct{})
		q.Handle("victim", func(ctx context.Context, _ []byte) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil
		})
		if err := q.Push(context.Background(), "victim", nil, Options{MaxTries: 5}); err != nil {
			t.Fatal(err)
		}

		stop := startWorker(t, q, fast(WorkerOptions{
			StuckAfter: time.Minute, Heartbeat: 20 * time.Millisecond, Timeout: 30 * time.Second,
		}))
		<-started

		// Another worker reclaimed then claimed it: the attempt counter moved.
		if err := db.Model(&models.Job{}).Where("1 = 1").Update("attempts", 2).Error; err != nil {
			t.Fatal(err)
		}

		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("the job was not cancelled after losing its claim")
		}
		eventually(t, 5*time.Second, "the loss to be counted", func() bool { return q.Stats().Lost == 1 })
		_ = stop()

		rows := jobRows(t, db)
		if len(rows) != 1 || rows[0].Status != models.JobRunning || rows[0].Attempts != 2 {
			t.Errorf("rows = %+v, want the other claim left untouched", rows)
		}
		if q.Stats().Processed != 0 {
			t.Error("a run with a lost claim was counted as processed")
		}
	})
}

// Problem d: reclaimed jobs count as an attempt and end in failed.
func TestReclaimFailsAJobThatKeepsKillingItsWorker(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		store := NewStore(db)
		stale := time.Now().Add(-time.Hour)
		key := "crasher"
		rows := []models.Job{
			{Name: "crasher", Queue: "default", Status: models.JobRunning, Attempts: 3, MaxTries: 3, ClaimedAt: &stale, UniqueKey: &key, RunAt: stale},
			{Name: "survivor", Queue: "default", Status: models.JobRunning, Attempts: 1, MaxTries: 3, ClaimedAt: &stale, RunAt: stale},
		}
		if err := db.Create(&rows).Error; err != nil {
			t.Fatal(err)
		}

		requeued, failed, err := store.Reclaim(context.Background(), time.Now().Add(-time.Minute))
		if err != nil || requeued != 1 || failed != 1 {
			t.Fatalf("Reclaim = %d, %d, %v; want 1, 1, nil", requeued, failed, err)
		}

		got := jobRows(t, db)
		if got[0].Status != models.JobFailed || got[0].LastError != LostWorker || got[0].UniqueKey != nil {
			t.Errorf("crasher = %s %q key %v, want failed, LostWorker, key released", got[0].Status, got[0].LastError, got[0].UniqueKey)
		}
		if got[1].Status != models.JobPending || got[1].Attempts != 1 {
			t.Errorf("survivor = %s/%d, want pending with its attempt kept", got[1].Status, got[1].Attempts)
		}
	})
}

// Problem f: a unique key is enqueued once while pending or running, and
// freed when the job completes or fails.
func TestAUniqueKeyIsEnqueuedOnce(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		q.Handle("quotes", func(context.Context, []byte) error { return errors.New("provider down") })
		ctx := context.Background()
		unique := Options{UniqueKey: "quotes:BTC-HTG", MaxTries: 1}

		for i, want := range []bool{true, false, false} {
			enqueued, err := q.Enqueue(ctx, "quotes", map[string]int{"i": i}, unique)
			if err != nil || enqueued != want {
				t.Fatalf("dispatch %d = %v, %v; want %v", i, enqueued, err, want)
			}
		}
		if other, err := q.Enqueue(ctx, "quotes", nil, Options{UniqueKey: "quotes:USDT-HTG"}); err != nil || !other {
			t.Fatalf("another key = %v, %v; want enqueued", other, err)
		}
		if plain, err := q.Enqueue(ctx, "quotes", nil); err != nil || !plain {
			t.Fatalf("no key = %v, %v; want enqueued", plain, err)
		}

		entry, err := q.Store().Claim(ctx, []string{"default"}, time.Now())
		if err != nil || entry == nil || entry.UniqueKey != "quotes:BTC-HTG" {
			t.Fatalf("claim = %+v, %v", entry, err)
		}
		if again, _ := q.Enqueue(ctx, "quotes", nil, unique); again {
			t.Fatal("a running unique job was enqueued again")
		}

		if err := q.Store().Fail(ctx, entry, "provider down"); err != nil {
			t.Fatal(err)
		}
		if again, err := q.Enqueue(ctx, "quotes", nil, unique); err != nil || !again {
			t.Fatalf("after a failure = %v, %v; want the key free again", again, err)
		}
	})
}

// Concurrent dispatches of one key, from many goroutines (API replicas),
// write one row.
func TestConcurrentDispatchesOfAUniqueKeyWriteOneRow(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		q.Handle("quotes", func(context.Context, []byte) error { return nil })

		var enqueued atomic.Int32
		var waiting sync.WaitGroup
		for range 16 {
			waiting.Add(1)
			go func() {
				defer waiting.Done()
				ok, err := q.Enqueue(context.Background(), "quotes", nil, Options{UniqueKey: "corridor:X"})
				if err != nil {
					t.Errorf("enqueue: %v", err)
				}
				if ok {
					enqueued.Add(1)
				}
			}()
		}
		waiting.Wait()

		if enqueued.Load() != 1 || len(jobRows(t, db)) != 1 {
			t.Errorf("enqueued %d, rows %d; want 1 and 1", enqueued.Load(), len(jobRows(t, db)))
		}
	})
}

// Problem g: last_error is bounded and never repeats the payload.
func TestTheStoredErrorIsTruncatedAndHidesThePayload(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		q := New(NewStore(db))
		q.Handle("leaky", func(_ context.Context, payload []byte) error {
			return fmt.Errorf("provider refused %s: %s", payload, strings.Repeat("é", 4000))
		})
		if err := q.Push(context.Background(), "leaky", map[string]string{"wallet": "0xSECRETISH"}, Options{MaxTries: 1}); err != nil {
			t.Fatal(err)
		}

		stop := startWorker(t, q, fast(WorkerOptions{}))
		eventually(t, 10*time.Second, "the job to fail", func() bool { return q.Stats().Failed == 1 })
		_ = stop()

		job := jobRows(t, db)[0]
		if len(job.LastError) > MaxErrorLength {
			t.Errorf("last_error is %d bytes, want at most %d", len(job.LastError), MaxErrorLength)
		}
		if strings.Contains(job.LastError, "0xSECRETISH") {
			t.Error("last_error repeats the payload")
		}
		if !strings.Contains(job.LastError, "[payload]") || !strings.HasSuffix(job.LastError, "(truncated)") {
			t.Errorf("last_error = %.80q…", job.LastError)
		}
	})
}

func TestUnknownJobsFailAndCountersAreExported(t *testing.T) {
	db := sqliteFile(t)
	producer := New(NewStore(db))
	producer.Handle("ghost", func(context.Context, []byte) error { return nil })
	if err := producer.Push(context.Background(), "ghost", nil); err != nil {
		t.Fatal(err)
	}

	worker := New(NewStore(db))
	stop := startWorker(t, worker, fast(WorkerOptions{}))
	eventually(t, 5*time.Second, "the unknown job to fail", func() bool { return worker.Stats().Failed == 1 })
	_ = stop()

	var out bytes.Buffer
	worker.WriteMetrics(&out)
	for _, want := range []string{
		"omega_queue_jobs_failed_total 1",
		"omega_queue_jobs_processed_total 0",
		`omega_queue_jobs{status="failed"} 1`,
		`omega_queue_jobs{status="pending"} 0`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("metrics miss %q:\n%s", want, out.String())
		}
	}

	var empty bytes.Buffer
	New(nil).WriteMetrics(&empty)
	if strings.Contains(empty.String(), "omega_queue_jobs{") {
		t.Error("a queue without store reported a depth")
	}
}

// flaky fails the first writes of each kind, then delegates.
type flaky struct {
	Store
	mu       sync.Mutex
	failures int
}

func (f *flaky) Complete(ctx context.Context, entry *Entry) error {
	f.mu.Lock()
	if f.failures > 0 {
		f.failures--
		f.mu.Unlock()
		return errors.New("connection reset")
	}
	f.mu.Unlock()
	return f.Store.Complete(ctx, entry)
}

func TestATransientStoreErrorIsRetriedThenReported(t *testing.T) {
	for _, tc := range []struct {
		failures   int
		wantRows   int
		wantErrors int64
	}{
		{failures: 2, wantRows: 0, wantErrors: 0},
		{failures: 10, wantRows: 1, wantErrors: 1},
	} {
		t.Run(fmt.Sprint(tc.failures), func(t *testing.T) {
			db := sqliteFile(t)
			q := New(&flaky{Store: NewStore(db), failures: tc.failures})
			q.Handle("ok", func(context.Context, []byte) error { return nil })
			if err := q.Push(context.Background(), "ok", nil); err != nil {
				t.Fatal(err)
			}

			stop := startWorker(t, q, fast(WorkerOptions{}))
			eventually(t, 10*time.Second, "the job to be processed", func() bool { return q.Stats().Processed == 1 })
			eventually(t, 10*time.Second, "the write attempts to end", func() bool {
				if tc.wantErrors == 0 {
					return len(jobRows(t, db)) == 0
				}
				return q.Stats().StoreErrors == tc.wantErrors
			})
			_ = stop()

			if got := len(jobRows(t, db)); got != tc.wantRows {
				t.Errorf("rows = %d, want %d", got, tc.wantRows)
			}
			if got := q.Stats().StoreErrors; got != tc.wantErrors {
				t.Errorf("store errors = %d, want %d", got, tc.wantErrors)
			}
		})
	}
}

func TestWorkerOptionsRefuseATimeoutPastStuckAfter(t *testing.T) {
	q := New(NewStore(sqliteFile(t)))
	for _, opts := range []WorkerOptions{
		{StuckAfter: time.Minute, Timeout: time.Minute},
		{StuckAfter: time.Minute, Heartbeat: 2 * time.Minute},
	} {
		if err := q.Work(context.Background(), opts); !errors.Is(err, ErrOptions) {
			t.Errorf("Work(%+v) = %v, want ErrOptions", opts, err)
		}
	}

	defaults := WorkerOptions{}
	if err := defaults.defaults(); err != nil {
		t.Fatal(err)
	}
	if defaults.Timeout != 150*time.Second || defaults.Heartbeat != 75*time.Second || defaults.ShutdownTimeout != 30*time.Second {
		t.Errorf("defaults = %+v", defaults)
	}
}

// A store whose claim only ever errors must not spin, and must stop.
type brokenClaim struct{ Store }

func (brokenClaim) Claim(context.Context, []string, time.Time) (*Entry, error) {
	return nil, errors.New("database is down")
}

func TestDrainReturnsAnErrorWhenAJobIgnoresItsContext(t *testing.T) {
	previous := drainGrace
	drainGrace = 20 * time.Millisecond
	defer func() { drainGrace = previous }()

	db := sqliteFile(t)
	q := New(NewStore(db))
	release := make(chan struct{})
	started := make(chan struct{})
	q.Handle("deaf", func(context.Context, []byte) error {
		close(started)
		<-release
		return nil
	})
	if err := q.Push(context.Background(), "deaf", nil); err != nil {
		t.Fatal(err)
	}

	stop := startWorker(t, q, fast(WorkerOptions{ShutdownTimeout: 10 * time.Millisecond}))
	<-started
	if err := stop(); err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Errorf("stop = %v, want the abandoned jobs reported", err)
	}
	close(release)
	eventually(t, 5*time.Second, "the late job to record its outcome", func() bool { return len(jobRows(t, db)) == 0 })

	broken := New(brokenClaim{NewStore(db)})
	stopBroken := startWorker(t, broken, fast(WorkerOptions{}))
	time.Sleep(30 * time.Millisecond)
	if err := stopBroken(); err != nil {
		t.Errorf("stop = %v", err)
	}
}
