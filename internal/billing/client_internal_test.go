package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return New(Config{
		SecretKey:   "sk_test_whatever",
		BaseURL:     server.URL,
		MaxAttempts: 3,
		Backoff:     time.Millisecond,
	})
}

func TestTheRequestCarriesWhatStripeExpects(t *testing.T) {
	var seen *http.Request

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = r
		_, _ = w.Write([]byte(`{"id":"cus_1","email":"a@b.c"}`))
	})

	customer, err := client.CreateCustomer(context.Background(), "a@b.c", "A", "7", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if customer.ID != "cus_1" {
		t.Fatalf("the answer was decoded wrong: %+v", customer)
	}

	if got := seen.Header.Get("Authorization"); got != "Bearer sk_test_whatever" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := seen.Header.Get("Stripe-Version"); got != DefaultAPIVersion {
		t.Fatalf("Stripe-Version = %q, the version is not pinned", got)
	}
	if got := seen.Header.Get("Idempotency-Key"); got != "key-1" {
		t.Fatalf("Idempotency-Key = %q", got)
	}
	if got := seen.PostForm.Get("metadata[user_id]"); got != "7" {
		t.Fatalf("metadata[user_id] = %q, the customer is not tied to a user", got)
	}
}

func TestAWriteWithoutAnIdempotencyKeyIsRefusedBeforeItLeaves(t *testing.T) {
	var calls atomic.Int32

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"cus_1"}`))
	})

	if _, err := client.CreateCustomer(context.Background(), "a@b.c", "A", "7", "  "); err == nil {
		t.Fatal("a write went out with no idempotency key")
	}
	if calls.Load() != 0 {
		t.Fatalf("the request was sent anyway (%d calls)", calls.Load())
	}
}

func TestATransientFailureIsRetried(t *testing.T) {
	var calls atomic.Int32

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":"cus_1"}`))
	})

	if _, err := client.CreateCustomer(context.Background(), "a@b.c", "A", "7", "key-1"); err != nil {
		t.Fatalf("two 502s should not have been fatal: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", calls.Load())
	}
}

func TestARefusalIsNotRetried(t *testing.T) {
	var calls atomic.Int32

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such price"}}`))
	})

	_, err := client.CreateCheckoutSession(context.Background(), CheckoutParams{PriceID: "price_gone"}, "key-1")
	if err == nil {
		t.Fatal("a 400 was taken for a success")
	}

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("the Stripe error was not decoded: %v", err)
	}
	if failure.Code != "resource_missing" || failure.Message != "No such price" {
		t.Fatalf("the error was decoded wrong: %+v", failure)
	}
	if calls.Load() != 1 {
		t.Fatalf("a 400 was retried %d times", calls.Load()-1)
	}
}

func TestRateLimitingIsRetried(t *testing.T) {
	var calls atomic.Int32

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":"sub_1","status":"active"}`))
	})

	if _, err := client.GetSubscription(context.Background(), "sub_1"); err != nil {
		t.Fatalf("a 429 should have been retried: %v", err)
	}
}

func TestWithoutASecretKeyEveryCallSaysSoPlainly(t *testing.T) {
	client := New(Config{})

	if client.Configured() {
		t.Fatal("a client with no key claims to be configured")
	}
	if _, err := client.GetSubscription(context.Background(), "sub_1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestCancellingAtPeriodEndDoesNotDeleteTheSubscription(t *testing.T) {
	var seen *http.Request

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = r
		_, _ = w.Write([]byte(`{"id":"sub_1","status":"active","cancel_at_period_end":true}`))
	})

	subscription, err := client.CancelSubscription(context.Background(), "sub_1", false, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/v1/subscriptions/sub_1" {
		t.Fatalf("path = %q, an immediate cancellation was sent instead", seen.URL.Path)
	}
	if got := seen.PostForm.Get("cancel_at_period_end"); got != "true" {
		t.Fatalf("cancel_at_period_end = %q", got)
	}
	if !subscription.CancelAtPeriodEnd {
		t.Fatal("the answer was decoded wrong")
	}
}

func TestCancellingImmediatelyGoesToTheCancelEndpoint(t *testing.T) {
	var path string

	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"sub_1","status":"canceled"}`))
	})

	if _, err := client.CancelSubscription(context.Background(), "sub_1", true, "key-1"); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/subscriptions/sub_1/cancel" {
		t.Fatalf("path = %q", path)
	}
}

func TestASubscriptionReadsItsPriceAndItsDates(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"sub_1","customer":"cus_1","status":"trialing",
			"current_period_end":1735689600,"ended_at":0,
			"items":{"data":[{"price":{"id":"price_pro"}}]}}`))
	})

	subscription, err := client.GetSubscription(context.Background(), "sub_1")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.PriceID() != "price_pro" {
		t.Fatalf("PriceID = %q", subscription.PriceID())
	}
	if subscription.PeriodEnd() == nil || subscription.PeriodEnd().Unix() != 1735689600 {
		t.Fatalf("PeriodEnd = %v", subscription.PeriodEnd())
	}
	if subscription.Ended() != nil {
		t.Fatalf("Ended = %v, want nil while it runs", subscription.Ended())
	}
	if !Active(subscription.Status) {
		t.Fatal("a trialing subscription must count as active")
	}
}

func TestWhichStatusesEntitleTheOwner(t *testing.T) {
	for status, want := range map[string]bool{
		"active": true, "trialing": true, "past_due": true,
		"canceled": false, "unpaid": false, "incomplete": false,
		"incomplete_expired": false, "paused": false, "": false,
	} {
		if got := Active(status); got != want {
			t.Errorf("Active(%q) = %t, want %t", status, got, want)
		}
	}
}
