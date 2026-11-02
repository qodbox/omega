package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestEveryIsAlignedOnTheClock(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 17, 42, 0, time.UTC)
	cases := map[time.Duration]time.Time{
		5 * time.Second: time.Date(2026, 3, 1, 10, 17, 45, 0, time.UTC),
		time.Minute:     time.Date(2026, 3, 1, 10, 18, 0, 0, time.UTC),
		time.Hour:       time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
		6 * time.Hour:   time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	for every, want := range cases {
		if got := nextEvery(from, every); !got.Equal(want) {
			t.Errorf("nextEvery(%v) = %v, want %v", every, got, want)
		}
	}

	s := New(silent).Every("default", 0, func(context.Context) error { return nil })
	if s.entries[0].every != time.Minute {
		t.Errorf("a zero interval became %v, want a minute", s.entries[0].every)
	}
}

func TestATaskIsCancelledAtItsTimeout(t *testing.T) {
	var err atomic.Value
	s := New(silent)
	s.Every("bounded", time.Hour, func(ctx context.Context) error {
		<-ctx.Done()
		err.Store(ctx.Err())
		return ctx.Err()
	}).WithTimeout(20 * time.Millisecond)

	if s.entries[0].timeout != 20*time.Millisecond {
		t.Fatalf("timeout = %v", s.entries[0].timeout)
	}
	s.DailyAt("nightly", "03:00", func(context.Context) error { return nil })
	if s.entries[1].timeout != dailyTimeout {
		t.Errorf("daily default timeout = %v", s.entries[1].timeout)
	}

	s.mu.Lock()
	s.entries[0].nextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.tick(context.Background(), time.Now())
	s.inflight.Wait()

	if got, _ := err.Load().(error); !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("task saw %v, want its deadline", got)
	}
	if s.entries[0].failures != 1 {
		t.Errorf("failures = %d, want 1", s.entries[0].failures)
	}
}

func TestAPanickingTaskIsRecordedAndTheSchedulerSurvives(t *testing.T) {
	var after atomic.Bool
	s := New(silent)
	s.Every("panics", time.Minute, func(context.Context) error { panic("boom") })
	s.Every("after", time.Minute, func(context.Context) error { after.Store(true); return nil })

	s.mu.Lock()
	for _, item := range s.entries {
		item.nextRun = time.Now().Add(-time.Second)
	}
	s.mu.Unlock()
	s.tick(context.Background(), time.Now())
	s.inflight.Wait()

	if s.entries[0].failures != 1 || !after.Load() {
		t.Errorf("failures %d, other task ran %v", s.entries[0].failures, after.Load())
	}
}

func TestAllowOverlapStartsASecondRun(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int32
	s := New(silent)
	s.Every("overlapping", time.Minute, func(context.Context) error {
		started.Add(1)
		<-release
		return nil
	}).AllowOverlap()

	for range 2 {
		s.mu.Lock()
		s.entries[0].nextRun = time.Now().Add(-time.Second)
		s.mu.Unlock()
		s.tick(context.Background(), time.Now())
	}
	waitUntil(t, func() bool { return started.Load() == 2 })
	close(release)
	s.inflight.Wait()
}

func TestRunWaitsForTheRunningTaskOnShutdown(t *testing.T) {
	started := make(chan struct{})
	var finished, cancelled atomic.Bool
	s := New(silent).ShutdownTimeout(5 * time.Second)
	s.Every("slow", time.Second, func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			cancelled.Store(true)
		case <-time.After(100 * time.Millisecond):
			finished.Store(true)
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never started")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !finished.Load() || cancelled.Load() {
		t.Errorf("finished %v cancelled %v, want the run to complete", finished.Load(), cancelled.Load())
	}
}

func TestRunCancelsTasksPastTheShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	var cancelled atomic.Bool
	s := New(silent).ShutdownTimeout(20 * time.Millisecond)
	s.Every("stubborn", time.Second, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return ctx.Err()
	}).WithTimeout(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !cancelled.Load() {
		t.Error("the task was not cancelled past the shutdown timeout")
	}
}

func TestRunTaskRunsOneTaskNow(t *testing.T) {
	var ran atomic.Bool
	s := New(silent)
	s.Every("b-task", time.Hour, func(context.Context) error { ran.Store(true); return nil })
	s.Every("a-panics", time.Hour, func(context.Context) error { panic("boom") })

	if names := s.Names(); len(names) != 2 || names[0] != "a-panics" {
		t.Errorf("Names = %v", names)
	}
	if err := s.RunTask(context.Background(), "b-task"); err != nil || !ran.Load() {
		t.Errorf("RunTask = %v, ran %v", err, ran.Load())
	}
	if err := s.RunTask(context.Background(), "a-panics"); err == nil {
		t.Error("a panic was not reported")
	}
	if err := s.RunTask(context.Background(), "nope"); !errors.Is(err, ErrUnknownTask) {
		t.Errorf("unknown = %v", err)
	}
}
