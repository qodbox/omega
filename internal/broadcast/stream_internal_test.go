package broadcast

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("delai depasse en attendant: %s", what)
}

func streamApp(hub *Hub) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/events", hub.Stream(20*time.Millisecond))
	return app
}

func TestStreamRegistersThenReleasesItsSubscriber(t *testing.T) {
	hub := New()
	app := streamApp(hub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = app.Test(httptest.NewRequest("GET", "/events", nil), 2000)
	}()

	waitFor(t, "l'abonne s'enregistre", func() bool { return hub.Count() == 1 })

	hub.Shutdown()

	waitFor(t, "l'abonne se retire apres Shutdown", func() bool { return hub.Count() == 0 })
	<-done
}

func TestStreamSubscribesToTheRequestedChannels(t *testing.T) {
	hub := New()
	app := streamApp(hub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = app.Test(httptest.NewRequest("GET", "/events?channels=mon%20canal,%20autre%20", nil), 2000)
	}()

	waitFor(t, "l'abonne s'enregistre", func() bool { return hub.Count() == 1 })

	if delivered := hub.Publish("mon canal", "tick", 1); delivered != 1 {
		t.Errorf("canal avec espace interne: %d destinataire(s), want 1", delivered)
	}
	if delivered := hub.Publish("autre", "tick", 1); delivered != 1 {
		t.Errorf("canal apres espaces de bord: %d destinataire(s), want 1", delivered)
	}
	if delivered := hub.Publish("moncanal", "tick", 1); delivered != 0 {
		t.Errorf("un canal sans espace a recu: %d, want 0", delivered)
	}

	hub.Shutdown()
	waitFor(t, "l'abonne se retire", func() bool { return hub.Count() == 0 })
	<-done
}

func TestStreamWithoutChannelsReceivesEverything(t *testing.T) {
	hub := New()
	app := streamApp(hub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = app.Test(httptest.NewRequest("GET", "/events", nil), 2000)
	}()

	waitFor(t, "l'abonne s'enregistre", func() bool { return hub.Count() == 1 })

	if delivered := hub.Publish("n-importe-quoi", "tick", 1); delivered != 1 {
		t.Errorf("un abonne sans filtre n'a pas recu: %d", delivered)
	}

	hub.Shutdown()
	waitFor(t, "l'abonne se retire", func() bool { return hub.Count() == 0 })
	<-done
}

func TestShutdownIsSafeTwiceAndWithoutSubscribers(t *testing.T) {
	hub := New()
	hub.Shutdown()
	hub.Shutdown()

	app := streamApp(hub)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = app.Test(httptest.NewRequest("GET", "/events", nil), 2000)
	}()

	waitFor(t, "l'abonne s'enregistre", func() bool { return hub.Count() == 1 })
	hub.Shutdown()
	hub.Shutdown()

	waitFor(t, "l'abonne se retire", func() bool { return hub.Count() == 0 })
	<-done
}
