package cache

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func covFastCache(t *testing.T) *Cache {
	t.Helper()

	c := &Cache{
		items:   map[string]item{},
		janitor: time.NewTicker(time.Millisecond),
		stop:    make(chan struct{}),
		flights: map[string]*flight{},
	}
	go c.sweep()
	t.Cleanup(c.Close)

	return c
}

func TestSweepDropsWhatHasExpired(t *testing.T) {
	c := covFastCache(t)

	c.mu.Lock()
	c.items["perime"] = item{value: []byte(`"x"`), expires: time.Now().Add(-time.Hour)}
	c.items["permanent"] = item{value: []byte(`"x"`)}
	c.items["plus_tard"] = item{value: []byte(`"x"`), expires: time.Now().Add(time.Hour)}
	c.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.RLock()
		_, stillThere := c.items["perime"]
		c.mu.RUnlock()

		if !stillThere {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("l'entree perimee n'a pas ete balayee")
		}
		time.Sleep(2 * time.Millisecond)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if _, ok := c.items["permanent"]; !ok {
		t.Error("une entree sans expiration a ete balayee")
	}
	if _, ok := c.items["plus_tard"]; !ok {
		t.Error("une entree encore valide a ete balayee")
	}
}

func TestCloseStopsTheSweeper(t *testing.T) {
	c := &Cache{
		items:   map[string]item{},
		janitor: time.NewTicker(time.Millisecond),
		stop:    make(chan struct{}),
		flights: map[string]*flight{},
	}

	done := make(chan struct{})
	go func() {
		c.sweep()
		close(done)
	}()

	c.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep tourne toujours apres Close")
	}
}

func TestPutRefusesWhatJsonCannotEncode(t *testing.T) {
	c := New()
	defer c.Close()

	if err := c.Put("cle", make(chan int), time.Minute); err == nil {
		t.Fatal("une valeur non serialisable doit echouer")
	}

	c.mu.RLock()
	_, stored := c.items["cle"]
	c.mu.RUnlock()

	if stored {
		t.Error("rien ne doit etre garde quand l'encodage echoue")
	}
}

func TestRememberReturnsTheLoaderFailure(t *testing.T) {
	c := New()
	defer c.Close()

	sentinel := errors.New("source indisponible")
	calls := 0

	value, err := Remember(c, "cle", time.Minute, func() (string, error) {
		calls++
		return "", sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("erreur = %v, attendu celle du chargeur", err)
	}
	if value != "" {
		t.Errorf("valeur = %q, attendu vide", value)
	}

	if _, err := Remember(c, "cle", time.Minute, func() (string, error) {
		calls++
		return "", sentinel
	}); err == nil {
		t.Fatal("un echec ne doit pas etre mis en cache")
	}
	if calls != 2 {
		t.Errorf("le chargeur a ete appele %d fois, attendu 2", calls)
	}
}

func TestRememberServesTheCachedValueWithoutRebuilding(t *testing.T) {
	c := New()
	defer c.Close()

	calls := 0
	build := func() (string, error) {
		calls++
		return "valeur", nil
	}

	for range 3 {
		got, err := Remember(c, "cle", time.Minute, build)
		if err != nil {
			t.Fatalf("Remember: %v", err)
		}
		if got != "valeur" {
			t.Errorf("valeur = %q", got)
		}
	}

	if calls != 1 {
		t.Errorf("le chargeur a tourne %d fois, attendu 1", calls)
	}
}

func TestRememberBuildsOnceUnderConcurrency(t *testing.T) {
	c := New()
	defer c.Close()

	var mu sync.Mutex
	calls := 0

	start := make(chan struct{})
	var wg sync.WaitGroup

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			_, _ = Remember(c, "partagee", time.Minute, func() (string, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				return "valeur", nil
			})
		}()
	}

	close(start)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("20 lecteurs simultanes ont declenche %d constructions, attendu 1", calls)
	}
}

func TestRememberReportsAValueItCannotStore(t *testing.T) {
	c := New()
	defer c.Close()

	_, err := Remember(c, "cle", time.Minute, func() (chan int, error) {
		return make(chan int), nil
	})

	if err == nil {
		t.Fatal("une valeur non serialisable doit remonter depuis Remember")
	}
}
