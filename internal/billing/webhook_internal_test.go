package billing

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "whsec_a_secret_long_enough_to_sign_with"

func payload() []byte {
	return []byte(`{"id":"evt_1","type":"customer.subscription.updated","created":1,"livemode":false,` +
		`"data":{"object":{"id":"sub_1","customer":"cus_1","status":"active"}}}`)
}

func TestVerifyWebhookAcceptsWhatStripeWouldSend(t *testing.T) {
	body := payload()
	header := Sign(body, testSecret, time.Now())

	event, err := VerifyWebhook(body, header, testSecret, DefaultTolerance)
	if err != nil {
		t.Fatalf("a genuine signature was refused: %v", err)
	}
	if event.ID != "evt_1" || event.Type != "customer.subscription.updated" {
		t.Fatalf("the event was decoded wrong: %+v", event)
	}
	if !strings.Contains(string(event.Data), `"sub_1"`) {
		t.Fatalf("data.object was not carried through: %s", event.Data)
	}
}

func TestVerifyWebhookRefusesATamperedBody(t *testing.T) {
	body := payload()
	header := Sign(body, testSecret, time.Now())

	// One byte of the payload changes, the signature does not.
	tampered := []byte(strings.Replace(string(body), `"active"`, `"trialing"`, 1))

	if _, err := VerifyWebhook(tampered, header, testSecret, DefaultTolerance); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("a tampered body was accepted: %v", err)
	}
}

func TestVerifyWebhookRefusesAnotherSecret(t *testing.T) {
	body := payload()
	header := Sign(body, "whsec_someone_elses_secret_entirely", time.Now())

	if _, err := VerifyWebhook(body, header, testSecret, DefaultTolerance); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("a signature from another secret was accepted: %v", err)
	}
}

func TestVerifyWebhookRefusesAReplayOutsideTheTolerance(t *testing.T) {
	body := payload()
	// A perfectly valid signature — from an hour ago.
	header := Sign(body, testSecret, time.Now().Add(-time.Hour))

	if _, err := VerifyWebhook(body, header, testSecret, 5*time.Minute); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("an hour-old request was accepted: %v", err)
	}
}

func TestVerifyWebhookRefusesATimestampFromTheFuture(t *testing.T) {
	body := payload()
	header := Sign(body, testSecret, time.Now().Add(time.Hour))

	if _, err := VerifyWebhook(body, header, testSecret, 5*time.Minute); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("a timestamp an hour ahead was accepted: %v", err)
	}
}

func TestVerifyWebhookAcceptsOneOfSeveralSignaturesDuringARotation(t *testing.T) {
	body := payload()
	now := time.Now()

	// Stripe sends every signature it holds while a secret is rotated: one
	// timestamp, then a v1 per secret. Ours is the second one here.
	stale := Sign(body, "whsec_the_secret_being_retired_now", now)
	_, mine, err := parseSignatureHeader(Sign(body, testSecret, now))
	if err != nil {
		t.Fatal(err)
	}
	header := stale + ",v1=" + mine[0]

	if _, err := VerifyWebhook(body, header, testSecret, DefaultTolerance); err != nil {
		t.Fatalf("a rotation header was refused: %v", err)
	}
}

func TestVerifyWebhookRefusesAHeaderItCannotRead(t *testing.T) {
	body := payload()

	for name, header := range map[string]string{
		"empty":            "",
		"no timestamp":     "v1=deadbeef",
		"no signature":     "t=1700000000",
		"unreadable time":  "t=yesterday,v1=deadbeef",
		"only the v0 kind": "t=1700000000,v0=deadbeef",
	} {
		if _, err := VerifyWebhook(body, header, testSecret, DefaultTolerance); err == nil {
			t.Fatalf("%s: a header that cannot be read was accepted", name)
		}
	}
}

func TestVerifyWebhookRefusesEverythingWithoutASecret(t *testing.T) {
	body := payload()
	header := Sign(body, testSecret, time.Now())

	if _, err := VerifyWebhook(body, header, "", DefaultTolerance); !errors.Is(err, ErrNoWebhookSecret) {
		t.Fatalf("an unconfigured endpoint accepted a webhook: %v", err)
	}
}

func TestParseEventRefusesAPayloadThatIsNotAnEvent(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":  `{"id":`,
		"no id":     `{"type":"customer.subscription.updated"}`,
		"no type":   `{"id":"evt_1"}`,
		"an object": `{"id":"sub_1","object":"subscription"}`,
	} {
		if _, err := ParseEvent([]byte(raw)); err == nil {
			t.Fatalf("%s: it was taken for an event", name)
		}
	}
}
