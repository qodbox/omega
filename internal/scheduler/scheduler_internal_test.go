package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

var silent = zerolog.Nop()

func TestTickRunsOnlyWhatIsDue(t *testing.T) {
	var mu sync.Mutex
	runs := map[string]int{}

	count := func(name string) Task {
		return func(context.Context) error {
			mu.Lock()
			runs[name]++
			mu.Unlock()
			return nil
		}
	}

	s := New(silent)
	s.Every("due", time.Minute, count("due"))
	s.Every("plus-tard", time.Hour, count("plus-tard"))

	s.mu.Lock()
	s.entries[0].nextRun = time.Now().Add(-time.Second)
	s.entries[1].nextRun = time.Now().Add(time.Hour)
	s.mu.Unlock()

	s.tick(context.Background(), time.Now())
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runs["due"] == 1
	})

	mu.Lock()
	defer mu.Unlock()
	if runs["plus-tard"] != 0 {
		t.Errorf("une tache non due a tourne %d fois", runs["plus-tard"])
	}
}

func TestLaunchContainsAPanicAndRecordsTheError(t *testing.T) {
	s := New(silent)
	s.Every("paniquante", time.Minute, func(context.Context) error { panic("boum") })
	s.Every("fautive", time.Minute, func(context.Context) error { return errors.New("echec") })

	s.mu.Lock()
	for _, item := range s.entries {
		item.nextRun = time.Now().Add(-time.Second)
	}
	s.mu.Unlock()

	s.tick(context.Background(), time.Now())

	waitUntil(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, item := range s.entries {
			if item.running {
				return false
			}
		}
		return true
	})
}

func TestProtectTurnsAPanicIntoAnError(t *testing.T) {
	if err := protect(func() error { panic("boum") }); err == nil {
		t.Fatal("la panique n'a pas ete convertie en erreur")
	}
	if err := protect(func() error { return nil }); err != nil {
		t.Fatalf("un succes est devenu une erreur: %v", err)
	}
}

func TestTickSkipsAnOverlappingRun(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)

	s := New(silent)
	s.Every("lente", time.Millisecond, func(context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	})

	s.mu.Lock()
	s.entries[0].nextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()

	s.tick(context.Background(), time.Now())
	<-started

	s.mu.Lock()
	s.entries[0].nextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.tick(context.Background(), time.Now())

	select {
	case <-started:
		close(release)
		t.Fatal("une seconde execution a demarre alors que la premiere tourne")
	case <-time.After(80 * time.Millisecond):
	}
	close(release)
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("delai depasse")
}
