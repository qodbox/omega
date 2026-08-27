package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultTolerance is how far a webhook's timestamp may drift from our clock
// before it is refused. It bounds the window in which a captured request could
// be replayed, while leaving room for ordinary clock skew.
const DefaultTolerance = 5 * time.Minute

var (
	ErrNoSignature      = errors.New("billing: the request carries no Stripe-Signature header")
	ErrSignatureFormat  = errors.New("billing: malformed Stripe-Signature header")
	ErrSignatureExpired = errors.New("billing: the webhook timestamp is outside the tolerance")
	ErrSignatureInvalid = errors.New("billing: no signature in the header matches the payload")
	ErrNoWebhookSecret  = errors.New("billing: no Stripe webhook secret configured")
)

// Event is the part of a Stripe event this layer acts on. Data stays raw: what
// it holds depends on Type, and each handler decodes the shape it expects.
type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Created int64           `json:"created"`
	Data    json.RawMessage `json:"-"`

	// Livemode separates a real payment from a test one. Acting on a test event
	// in production would hand out a paid plan for free.
	Livemode bool `json:"livemode"`
}

// object pulls data.object out of the envelope.
type eventEnvelope struct {
	Event
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// VerifyWebhook authenticates a raw request body against its Stripe-Signature
// header and returns the event it carries.
//
// The body must be the bytes Stripe sent, byte for byte: the signature covers
// them exactly, so anything that re-encodes JSON before this point invalidates
// it. Everything here is decided on the raw payload, before it is parsed.
func VerifyWebhook(payload []byte, header, secret string, tolerance time.Duration) (Event, error) {
	if strings.TrimSpace(secret) == "" {
		return Event{}, ErrNoWebhookSecret
	}
	if strings.TrimSpace(header) == "" {
		return Event{}, ErrNoSignature
	}
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}

	timestamp, signatures, err := parseSignatureHeader(header)
	if err != nil {
		return Event{}, err
	}

	// The timestamp is checked first, and it is part of the signed payload:
	// an attacker cannot move it without invalidating every signature below.
	if drift := time.Since(time.Unix(timestamp, 0)); drift > tolerance || drift < -tolerance {
		return Event{}, ErrSignatureExpired
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)

	// Stripe sends several v1 signatures while a secret is being rotated; the
	// request is genuine if any one of them matches.
	matched := false
	for _, candidate := range signatures {
		raw, err := hex.DecodeString(candidate)
		if err != nil {
			continue
		}
		if hmac.Equal(raw, expected) {
			matched = true
		}
	}
	if !matched {
		return Event{}, ErrSignatureInvalid
	}

	return ParseEvent(payload)
}

// ParseEvent decodes an event body. It does not authenticate anything: call it
// only on a payload VerifyWebhook has already accepted.
func ParseEvent(payload []byte) (Event, error) {
	var envelope eventEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Event{}, fmt.Errorf("billing: decoding the event: %w", err)
	}
	if strings.TrimSpace(envelope.ID) == "" || strings.TrimSpace(envelope.Type) == "" {
		return Event{}, errors.New("billing: the payload is not a Stripe event")
	}

	event := envelope.Event
	event.Data = envelope.Data.Object
	return event, nil
}

// parseSignatureHeader splits "t=...,v1=...,v1=..." into its timestamp and its
// v1 signatures. Schemes other than v1 are ignored: v0 signs a different
// payload and accepting it would weaken the check.
func parseSignatureHeader(header string) (timestamp int64, signatures []string, err error) {
	var seenTimestamp bool

	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			parsed, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				return 0, nil, ErrSignatureFormat
			}
			timestamp, seenTimestamp = parsed, true
		case "v1":
			if value != "" {
				signatures = append(signatures, value)
			}
		}
	}

	if !seenTimestamp || len(signatures) == 0 {
		return 0, nil, ErrSignatureFormat
	}
	return timestamp, signatures, nil
}

// Sign builds the header Stripe would send for a payload. It exists so tests —
// and a local relay — can produce a request this package accepts.
func Sign(payload []byte, secret string, at time.Time) string {
	seconds := at.Unix()

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(seconds, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)

	return fmt.Sprintf("t=%d,v1=%s", seconds, hex.EncodeToString(mac.Sum(nil)))
}
