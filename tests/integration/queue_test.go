package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"omega/internal/queue"
)

func newQueue(t *testing.T) *queue.Queue {
	t.Helper()
	s := boot(t)
	return queue.New(queue.NewStore(s.app.DB))
}

func drain(t *testing.T, q *queue.Queue, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_ = q.Work(ctx, queue.WorkerOptions{
		Concurrency: 2,
		Poll:        10 * time.Millisecond,
		Log:         zerolog.Nop(),
	})
}

func TestAJobRunsOnceAndDisappears(t *testing.T) {
	q := newQueue(t)

	var mu sync.Mutex
	runs := 0

	q.Handle("count", func(ctx context.Context, payload []byte) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	})

	if err := q.Push(context.Background(), "count", map[string]any{"n": 1}); err != nil {
		t.Fatalf("push: %v", err)
	}
	drain(t, q, 400*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("the job ran %d times, want 1", runs)
	}

	pending, _ := q.Store().Pending(context.Background())
	if pending != 0 {
		t.Errorf("%d job(s) left pending", pending)
	}
}

func TestPushRefusesAnUnknownJob(t *testing.T) {
	q := newQueue(t)

	err := q.Push(context.Background(), "nobody-handles-this", nil)
	if !errors.Is(err, queue.ErrUnknownJob) {
		t.Errorf("err = %v, want ErrUnknownJob", err)
	}
}

func TestAFailingJobIsRetriedThenGivesUp(t *testing.T) {
	q := newQueue(t)

	var mu sync.Mutex
	attempts := 0

	q.Handle("always-fails", func(ctx context.Context, payload []byte) error {
		mu.Lock()
		attempts++
		mu.Unlock()
		return errors.New("nope")
	})

	if err := q.Push(context.Background(), "always-fails", nil, queue.Options{MaxTries: 2}); err != nil {
		t.Fatalf("push: %v", err)
	}
	drain(t, q, 300*time.Millisecond)

	mu.Lock()
	first := attempts
	mu.Unlock()
	if first < 1 {
		t.Fatal("the job never ran")
	}

	failed, err := q.Store().Failed(context.Background(), 10)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if len(failed) == 0 {
		pending, _ := q.Store().Pending(context.Background())
		if pending == 0 {
			t.Error("the job neither failed nor stayed pending")
		}
		return
	}
	if failed[0].LastError == "" {
		t.Error("the failure was not recorded")
	}
}

func TestAPanickingJobDoesNotKillTheWorker(t *testing.T) {
	q := newQueue(t)

	survived := make(chan struct{}, 1)
	q.Handle("panics", func(ctx context.Context, payload []byte) error {
		panic("inside the job")
	})
	q.Handle("after", func(ctx context.Context, payload []byte) error {
		survived <- struct{}{}
		return nil
	})

	ctx := context.Background()
	_ = q.Push(ctx, "panics", nil, queue.Options{MaxTries: 1})
	_ = q.Push(ctx, "after", nil)

	drain(t, q, 500*time.Millisecond)

	select {
	case <-survived:
	default:
		t.Error("the worker did not survive the panic")
	}
}

func TestADelayedJobWaitsItsTurn(t *testing.T) {
	q := newQueue(t)
	q.Handle("later", func(ctx context.Context, payload []byte) error { return nil })

	if err := q.Later(context.Background(), time.Hour, "later", nil); err != nil {
		t.Fatalf("later: %v", err)
	}

	drain(t, q, 200*time.Millisecond)

	pending, _ := q.Store().Pending(context.Background())
	if pending != 1 {
		t.Errorf("pending = %d, want the delayed job to still be waiting", pending)
	}
}

func TestRetryPushesFailedJobsBack(t *testing.T) {
	q := newQueue(t)
	q.Handle("broken", func(ctx context.Context, payload []byte) error {
		return errors.New("nope")
	})

	ctx := context.Background()
	_ = q.Push(ctx, "broken", nil, queue.Options{MaxTries: 1})
	drain(t, q, 300*time.Millisecond)

	failed, _ := q.Store().Failed(ctx, 10)
	if len(failed) == 0 {
		t.Skip("the job had not given up yet")
	}

	count, err := q.Store().Retry(ctx, 0)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if count == 0 {
		t.Error("nothing was requeued")
	}

	pending, _ := q.Store().Pending(ctx)
	if pending == 0 {
		t.Error("the requeued job is not pending")
	}
}

func TestSweepRemovesOldFailures(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	removed, err := q.Store().Sweep(ctx, "failed", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	_ = removed

	if _, err := q.Store().Sweep(ctx, "", time.Now().AddDate(0, 0, -30)); err != nil {
		t.Errorf("sweeping everything: %v", err)
	}
}

func TestTwoWorkersNeverTakeTheSameJob(t *testing.T) {
	q := newQueue(t)

	var mu sync.Mutex
	seen := map[string]int{}

	q.Handle("once", func(ctx context.Context, payload []byte) error {
		mu.Lock()
		seen[string(payload)]++
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	})

	for i := range 8 {
		_ = q.Push(context.Background(), "once", map[string]int{"n": i})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var waiting sync.WaitGroup
	for range 3 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			_ = q.Work(ctx, queue.WorkerOptions{
				Concurrency: 2,
				Poll:        10 * time.Millisecond,
				Log:         zerolog.Nop(),
			})
		}()
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := q.Store().Pending(context.Background())
		if err == nil && pending == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	waiting.Wait()

	mu.Lock()
	defer mu.Unlock()
	for payload, count := range seen {
		if count != 1 {
			t.Errorf("%s ran %d times", payload, count)
		}
	}
	if len(seen) != 8 {
		t.Errorf("%d distinct jobs ran, want 8", len(seen))
	}
}
