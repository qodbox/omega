package currency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// quoting builds an exchange served by a local provider, so the conversions
// below are exact rather than whatever the day brings. calls counts how often
// the provider was asked.
func quoting(t *testing.T, body string, cfg Config) (*Exchange, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if body == "" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	cfg.Endpoint = server.URL
	return New(cfg), &calls
}

func TestConvertMovesAnAmountAtTheDaysRate(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10,"JPY":160.0}}`, Config{})
	ctx := context.Background()

	if got := exchange.Convert(ctx, 1900, "USD"); got != 2090 {
		t.Fatalf("1900 EUR -> USD = %d, want 2090", got)
	}
	if got := exchange.Convert(ctx, 1900, "EUR"); got != 1900 {
		t.Fatalf("the base must not be converted, got %d", got)
	}
	// Half a minor unit rounds away from zero, in both directions.
	if got := exchange.Convert(ctx, 5, "USD"); got != 6 { // 5.5
		t.Fatalf("5 -> %d, want 6", got)
	}
	if got := exchange.Convert(ctx, -5, "USD"); got != -6 {
		t.Fatalf("-5 -> %d, want -6", got)
	}
}

func TestConvertFromCrossesTwoCurrenciesThroughTheBase(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":2.0,"GBP":0.5}}`, Config{})
	ctx := context.Background()

	// 100 USD is 50 EUR is 25 GBP.
	if got := exchange.ConvertFrom(ctx, 100, "USD", "GBP"); got != 25 {
		t.Fatalf("100 USD -> GBP = %d, want 25", got)
	}
	if got := exchange.ConvertFrom(ctx, 100, "USD", "EUR"); got != 50 {
		t.Fatalf("100 USD -> EUR = %d, want 50", got)
	}
	if got := exchange.ConvertFrom(ctx, 100, "EUR", "USD"); got != 200 {
		t.Fatalf("100 EUR -> USD = %d, want 200", got)
	}
	if got := exchange.ConvertFrom(ctx, 100, "USD", "USD"); got != 100 {
		t.Fatalf("the same currency must not be converted, got %d", got)
	}
}

func TestAnAmountIsNeverLostWhenNoRateIsKnown(t *testing.T) {
	// The provider is down and nothing was ever cached.
	exchange, _ := quoting(t, "", Config{})
	ctx := context.Background()

	if got := exchange.Convert(ctx, 1900, "USD"); got != 1900 {
		t.Fatalf("got %d: an unavailable rate must leave the amount alone, not zero it", got)
	}
	if got := exchange.ConvertFrom(ctx, 1900, "USD", "GBP"); got != 1900 {
		t.Fatalf("got %d", got)
	}

	rates := exchange.Rates(ctx)
	if len(rates) != 1 || rates["EUR"] != 1 {
		t.Fatalf("rates = %v, want the base alone", rates)
	}
	if _, ok := exchange.Rate(ctx, "USD"); ok {
		t.Fatal("a rate that does not exist was reported as usable")
	}
}

func TestTheLastKnownTableSurvivesTheProviderGoingDown(t *testing.T) {
	var down atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"rates":{"USD":1.10}}`))
	}))
	t.Cleanup(server.Close)

	exchange := New(Config{Endpoint: server.URL, TTL: time.Nanosecond})
	ctx := context.Background()

	if got := exchange.Convert(ctx, 1000, "USD"); got != 1100 {
		t.Fatalf("got %d", got)
	}

	down.Store(true)
	// The TTL has certainly passed, so this asks the provider and it fails.
	if got := exchange.Convert(ctx, 1000, "USD"); got != 1100 {
		t.Fatalf("got %d: yesterday's rate beats no rate at all", got)
	}
}

func TestTheProviderIsNotCalledWhileTheTableIsFresh(t *testing.T) {
	exchange, calls := quoting(t, `{"rates":{"USD":1.10}}`, Config{TTL: time.Hour})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		exchange.Rates(ctx)
	}
	if calls.Load() != 1 {
		t.Fatalf("the provider was called %d times for one fresh table", calls.Load())
	}

	if err := exchange.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("Refresh must ask whatever the cache holds, calls = %d", calls.Load())
	}
}

func TestRatesAreReadAndWrittenConcurrently(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10}}`, Config{TTL: time.Nanosecond})
	ctx := context.Background()

	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			exchange.Convert(ctx, 100, "USD")
			_ = exchange.Refresh(ctx)
		}()
	}
	wait.Wait()
}

func TestACallerCannotStoreACurrencyNobodyQuotes(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10}}`, Config{})

	for _, code := range []string{"WAT", "", "  ", "bitcoin"} {
		if exchange.Supports(code) {
			t.Errorf("%q was taken for a currency", code)
		}
		if got := exchange.Normalise(code); got != "EUR" {
			t.Errorf("Normalise(%q) = %q, want the base", code, got)
		}
	}

	if got := exchange.Normalise(" usd "); got != "USD" {
		t.Fatalf("Normalise(\" usd \") = %q", got)
	}
}

func TestTheCatalogueCanBeNarrowedButNeverLosesItsBase(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10}}`, Config{
		Base:      "GBP",
		Available: []string{"usd", "jpy"},
	})

	if exchange.Base() != "GBP" {
		t.Fatalf("Base = %q", exchange.Base())
	}
	if !exchange.Supports("GBP") {
		t.Fatal("an exchange that cannot quote its own base could convert nothing")
	}
	if !exchange.Supports("USD") || !exchange.Supports("JPY") {
		t.Fatal("a currency that was asked for is missing")
	}
	if exchange.Supports("CHF") {
		t.Fatal("a currency outside the configured list is on offer")
	}
	if len(exchange.Units()) != 3 {
		t.Fatalf("units = %d, want GBP, USD and JPY", len(exchange.Units()))
	}
}

func TestUnitsCannotBeMutatedByItsCaller(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10}}`, Config{})

	units := exchange.Units()
	units[0] = Unit{Code: "XXX"}

	if exchange.Units()[0].Code == "XXX" {
		t.Fatal("the catalogue was altered from outside")
	}
}

func TestFormatRendersAnAmountForAHuman(t *testing.T) {
	exchange, _ := quoting(t, `{"rates":{"USD":1.10}}`, Config{})

	for _, tc := range []struct {
		minor int64
		code  string
		want  string
	}{
		{1900, "EUR", "€19.00"},
		{1900, "USD", "$19.00"},
		{5, "EUR", "€0.05"},
		{-1900, "EUR", "-€19.00"},
		{1900, "JPY", "¥1900"},    // a currency with no minor unit
		{-1900, "JPY", "-¥1900"},  // the sign leads there too, never after the symbol
		{1900, "WAT", "WAT19.00"}, // unknown: rendered with its code, never dropped
	} {
		if got := exchange.Format(tc.minor, tc.code); got != tc.want {
			t.Errorf("Format(%d, %q) = %q, want %q", tc.minor, tc.code, got, tc.want)
		}
	}
}

func TestTheDefaultsAreUsable(t *testing.T) {
	exchange := New(Config{})

	if exchange.Base() != DefaultBase {
		t.Fatalf("Base = %q", exchange.Base())
	}
	if len(exchange.Units()) < 10 {
		t.Fatalf("units = %d, the default catalogue is too thin", len(exchange.Units()))
	}
	if !exchange.Supports("EUR") || !exchange.Supports("USD") {
		t.Fatal("the usual currencies are not on offer by default")
	}
}
