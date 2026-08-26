package broadcast

import "testing"

func TestSplitAndTrimKeepsInnerSpaces(t *testing.T) {
	got := splitAndTrim(" mon canal , autre ,, ")

	if len(got) != 2 || got[0] != "mon canal" || got[1] != "autre" {
		t.Fatalf("decoupage incorrect: %q", got)
	}
}

func TestPublishCountsDroppedMessages(t *testing.T) {
	hub := New()
	client := hub.subscribe([]string{"canal"})
	t.Cleanup(func() { hub.unsubscribe(client) })

	for i := 0; i < hub.buffer+5; i++ {
		hub.Publish("canal", "tick", i)
	}

	if hub.Dropped() != 5 {
		t.Fatalf("attendu 5 messages jetes, obtenu %d", hub.Dropped())
	}
}

func TestUnsubscribeRemovesTheClient(t *testing.T) {
	hub := New()
	client := hub.subscribe(nil)

	if hub.Count() != 1 {
		t.Fatalf("abonne non enregistre: %d", hub.Count())
	}

	hub.unsubscribe(client)

	if hub.Count() != 0 {
		t.Fatalf("abonne non retire: %d", hub.Count())
	}
}

func TestSocketSubscribesAndUnsubscribesItsChannels(t *testing.T) {
	hub := New()
	client := hub.subscribe(nil)
	t.Cleanup(func() { hub.unsubscribe(client) })

	socket := &Socket{client: client, hub: hub}

	socket.Subscribe("factures")
	if hub.Publish("factures", "tick", 1) != 1 {
		t.Fatal("le canal souscrit ne recoit rien")
	}
	if hub.Publish("autre", "tick", 1) != 0 {
		t.Fatal("un canal non souscrit recoit")
	}

	socket.Unsubscribe("factures")
	if hub.Publish("factures", "tick", 1) != 0 {
		t.Fatal("le canal desabonne recoit encore")
	}
}

func TestSocketSendIsANoOpOnceClosed(t *testing.T) {
	hub := New()
	client := hub.subscribe(nil)
	t.Cleanup(func() { hub.unsubscribe(client) })

	socket := &Socket{client: client, hub: hub, closed: true}

	if err := socket.Send("evenement", map[string]any{"a": 1}); err != nil {
		t.Fatalf("Send sur un socket ferme: %v", err)
	}
	if err := socket.Send("evenement", make(chan int)); err == nil {
		t.Fatal("une charge non serialisable a ete acceptee")
	}
}
