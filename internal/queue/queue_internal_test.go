package queue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Problem e: exponential, capped, jittered, and never negative.
func TestBackoffGrowsExponentiallyWithJitterUnderACap(t *testing.T) {
	for attempt, ceiling := range map[int]time.Duration{
		0: time.Second, 1: 2 * time.Second, 3: 8 * time.Second, 9: 512 * time.Second,
		10: backoffCap, 40: backoffCap, 63: backoffCap, 64: backoffCap, 1000: backoffCap, -1: backoffCap,
	} {
		seen := map[time.Duration]bool{}
		for range 200 {
			wait := backoff(attempt)
			if wait > ceiling || wait < ceiling*4/5 {
				t.Fatalf("backoff(%d) = %v, want within [%v, %v]", attempt, wait, ceiling*4/5, ceiling)
			}
			seen[wait] = true
		}
		if len(seen) < 2 {
			t.Errorf("backoff(%d) has no jitter", attempt)
		}
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q", got)
	}
	long := strings.Repeat("€", 1000)
	got := truncate(long, 100)
	if len(got) > 100 || !utf8.ValidString(got) || !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("truncate = %q (%d bytes)", got, len(got))
	}
}

func TestFailureLeavesShortPayloadsAlone(t *testing.T) {
	if got := failure(errors.New("bad {}"), "{}"); got != "bad {}" {
		t.Errorf("failure = %q", got)
	}
	if got := failure(errors.New(`bad {"id":42}`), `{"id":42}`); got != "bad [payload]" {
		t.Errorf("failure = %q", got)
	}
}

type recorder struct {
	Store
	pushed NewJob
}

func (r *recorder) Push(_ context.Context, job NewJob) (bool, error) {
	r.pushed = job
	return true, nil
}

func TestEnqueueCarriesEveryOption(t *testing.T) {
	store := &recorder{}
	q := New(store)
	q.Handle("refresh", func(context.Context, []byte) error { return nil })

	before := time.Now()
	ok, err := q.Enqueue(context.Background(), "refresh", map[string]string{"corridor": "USDT-HTG"}, Options{
		Queue: "quotes", Delay: time.Minute, MaxTries: 7, Timeout: 15 * time.Second, UniqueKey: "  quotes:USDT-HTG ",
	})
	if err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v", ok, err)
	}

	job := store.pushed
	if job.Queue != "quotes" || job.MaxTries != 7 || job.Timeout != 15*time.Second || job.UniqueKey != "quotes:USDT-HTG" {
		t.Errorf("job = %+v", job)
	}
	if job.RunAt.Before(before.Add(time.Minute)) {
		t.Errorf("run_at = %v, want a minute later", job.RunAt)
	}

	if _, err := New(nil).Enqueue(context.Background(), "refresh", nil); !errors.Is(err, ErrNoStore) {
		t.Errorf("without store = %v", err)
	}
	if _, err := q.Enqueue(context.Background(), "nobody", nil); !errors.Is(err, ErrUnknownJob) {
		t.Errorf("unknown = %v", err)
	}
	if err := q.Later(context.Background(), time.Second, "refresh", nil); err != nil {
		t.Errorf("later = %v", err)
	}
}
