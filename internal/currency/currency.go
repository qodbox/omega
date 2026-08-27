// Package currency converts amounts for display, and only for display.
//
// What is stored stays denominated in the base currency: converting in storage
// would make every total depend on the day it was written, and two sums over
// the same month would stop agreeing. A caller picks the currency they want to
// read figures in, the server serves the day's rates, and the conversion
// happens at the edge.
//
// What a payment provider charges is a separate question, answered by the
// billing layer — a rate from a central bank is not a price.
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Unit describes one currency on offer.
type Unit struct {
	Code     string `json:"code"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
}

// catalogue is every currency this package can convert into. All of them are
// quoted by the European Central Bank, which is what guarantees a rate exists
// for each: offering one the provider does not know would be offering a choice
// that converts nothing. A currency the ECB stops quoting is removed from here
// — the catalogue is a promise that conversion works, not a wish list.
var catalogue = []Unit{
	{"EUR", "€", "Euro", 2},
	{"USD", "$", "US dollar", 2},
	{"GBP", "£", "Pound sterling", 2},
	{"JPY", "¥", "Japanese yen", 0},
	{"CNY", "CN¥", "Chinese yuan", 2},
	{"CHF", "CHF", "Swiss franc", 2},
	{"CAD", "C$", "Canadian dollar", 2},
	{"AUD", "A$", "Australian dollar", 2},
	{"NZD", "NZ$", "New Zealand dollar", 2},
	{"HKD", "HK$", "Hong Kong dollar", 2},
	{"SGD", "S$", "Singapore dollar", 2},
	{"INR", "₹", "Indian rupee", 2},
	{"KRW", "₩", "South Korean won", 0},
	{"SEK", "kr", "Swedish krona", 2},
	{"NOK", "kr", "Norwegian krone", 2},
	{"DKK", "kr", "Danish krone", 2},
	{"PLN", "zł", "Polish zloty", 2},
	{"CZK", "Kč", "Czech koruna", 2},
	{"HUF", "Ft", "Hungarian forint", 2},
	{"RON", "lei", "Romanian leu", 2},
	{"ISK", "kr", "Icelandic krona", 0},
	{"MXN", "MX$", "Mexican peso", 2},
	{"BRL", "R$", "Brazilian real", 2},
	{"ZAR", "R", "South African rand", 2},
	{"TRY", "₺", "Turkish lira", 2},
	{"ILS", "₪", "Israeli shekel", 2},
	{"PHP", "₱", "Philippine peso", 2},
	{"MYR", "RM", "Malaysian ringgit", 2},
	{"THB", "฿", "Thai baht", 2},
	{"IDR", "Rp", "Indonesian rupiah", 0},
}

const (
	// DefaultBase is what amounts are stored in when nothing says otherwise.
	DefaultBase = "EUR"

	// DefaultEndpoint quotes the European Central Bank's daily reference rates.
	DefaultEndpoint = "https://api.frankfurter.dev/v1/latest"

	// DefaultTTL is how long a rate is served before it is fetched again. Rates
	// move once a day; a dashboard must not call a third party on every glance.
	DefaultTTL = 12 * time.Hour
)

// Config is what an exchange needs. Every field has a working default.
type Config struct {
	Base     string
	Endpoint string
	TTL      time.Duration
	Timeout  time.Duration

	// Available restricts the catalogue to these codes. Empty offers all of
	// them. The base currency is always offered, whatever this says.
	Available []string
}

// Exchange serves the day's rates and converts with them. It is safe for
// concurrent use, and it holds one table for the whole process.
type Exchange struct {
	base     string
	endpoint string
	ttl      time.Duration
	units    []Unit
	http     *http.Client

	mu      sync.RWMutex
	rates   map[string]float64
	fetched time.Time
}

// New builds an exchange, filling in the defaults.
func New(cfg Config) *Exchange {
	base := normalise(cfg.Base)
	if base == "" {
		base = DefaultBase
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	return &Exchange{
		base:     base,
		endpoint: endpoint,
		ttl:      ttl,
		units:    offered(base, cfg.Available),
		http:     &http.Client{Timeout: timeout},
	}
}

// Base is the currency amounts are stored in.
func (e *Exchange) Base() string { return e.base }

// Units is the catalogue on offer.
func (e *Exchange) Units() []Unit { return append([]Unit(nil), e.units...) }

// Unit returns one currency's description, and whether it is on offer.
func (e *Exchange) Unit(code string) (Unit, bool) {
	code = normalise(code)
	for _, unit := range e.units {
		if unit.Code == code {
			return unit, true
		}
	}
	return Unit{}, false
}

// Supports reports whether a currency is on offer.
func (e *Exchange) Supports(code string) bool {
	_, found := e.Unit(code)
	return found
}

// Normalise maps a code onto the catalogue, or onto the base currency. It is
// what turns whatever a client sent into something safe to store.
func (e *Exchange) Normalise(code string) string {
	if code = normalise(code); e.Supports(code) {
		return code
	}
	return e.base
}

// Rates returns the rates from the base currency, the base itself being 1.
//
// When the provider does not answer, the last table still held is served; with
// none at all, a table where only the base means anything — which is to say, no
// conversion. An unavailable rate is not a failure: the base beats nothing.
func (e *Exchange) Rates(ctx context.Context) map[string]float64 {
	e.mu.RLock()
	known, fresh := e.rates, e.rates != nil && time.Since(e.fetched) < e.ttl
	e.mu.RUnlock()

	if fresh {
		return copyOf(known)
	}

	fetched, err := e.fetch(ctx)
	if err != nil || len(fetched) == 0 {
		if known != nil {
			return copyOf(known)
		}
		return map[string]float64{e.base: 1}
	}

	e.mu.Lock()
	e.rates, e.fetched = fetched, time.Now()
	e.mu.Unlock()

	return copyOf(fetched)
}

// Rate returns the rate from the base currency to one other, and whether it is
// usable.
func (e *Exchange) Rate(ctx context.Context, to string) (float64, bool) {
	to = normalise(to)
	if to == e.base {
		return 1, true
	}
	rate, found := e.Rates(ctx)[to]
	return rate, found && rate > 0
}

// Refresh fetches the rates now, whatever the cache holds. The scheduler calls
// it, so that no visitor pays for the wait on a remote call.
func (e *Exchange) Refresh(ctx context.Context) error {
	fetched, err := e.fetch(ctx)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.rates, e.fetched = fetched, time.Now()
	e.mu.Unlock()
	return nil
}

// Convert moves a minor-unit amount — cents, and their equivalent elsewhere —
// from the base currency into another. Amounts travel in minor units and so
// does the conversion, so no rounding creeps in before display.
//
// With no usable rate the amount is returned unchanged, still in the base
// currency: the caller reads Rate when it needs to know which it got.
func (e *Exchange) Convert(ctx context.Context, minor int64, to string) int64 {
	rate, ok := e.Rate(ctx, to)
	if !ok {
		return minor
	}
	if rate == 1 {
		return minor
	}

	converted := float64(minor) * rate
	if converted < 0 {
		return int64(converted - 0.5)
	}
	return int64(converted + 0.5)
}

// ConvertFrom moves a minor-unit amount between two currencies, neither of
// which need be the base: the rates are quoted against the base, so the cross
// rate is one division away.
//
// With no usable rate for either side the amount is returned unchanged, still
// in the currency it came in.
func (e *Exchange) ConvertFrom(ctx context.Context, minor int64, from, to string) int64 {
	from, to = normalise(from), normalise(to)
	if from == to {
		return minor
	}

	rates := e.Rates(ctx)
	source, sourceOK := rates[from]
	target, targetOK := rates[to]

	if from == e.base {
		source, sourceOK = 1, true
	}
	if to == e.base {
		target, targetOK = 1, true
	}
	if !sourceOK || !targetOK || source <= 0 || target <= 0 {
		return minor
	}

	converted := float64(minor) * target / source
	if converted < 0 {
		return int64(converted - 0.5)
	}
	return int64(converted + 0.5)
}

// Format renders a minor-unit amount in one currency, for a human. A currency
// the catalogue does not know is rendered with its code, never dropped.
func (e *Exchange) Format(minor int64, code string) string {
	code = normalise(code)

	unit, found := e.Unit(code)
	if !found {
		unit = Unit{Code: code, Symbol: code, Decimals: 2}
	}

	// The sign leads, ahead of the symbol, whether or not the currency has a
	// minor unit: "-¥1900", never "¥-1900".
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}

	if unit.Decimals == 0 {
		return fmt.Sprintf("%s%s%d", sign, unit.Symbol, minor)
	}

	divisor := int64(1)
	for i := 0; i < unit.Decimals; i++ {
		divisor *= 10
	}
	return fmt.Sprintf("%s%s%d.%0*d", sign, unit.Symbol, minor/divisor, unit.Decimals, minor%divisor)
}

func (e *Exchange) fetch(ctx context.Context) (map[string]float64, error) {
	wanted := make([]string, 0, len(e.units))
	for _, unit := range e.units {
		if unit.Code != e.base {
			wanted = append(wanted, unit.Code)
		}
	}
	if len(wanted) == 0 {
		return map[string]float64{e.base: 1}, nil
	}

	query := url.Values{}
	query.Set("base", e.base)
	query.Set("symbols", strings.Join(wanted, ","))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	response, err := e.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("currency: the rate provider answered %d", response.StatusCode)
	}

	var body struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Rates) == 0 {
		return nil, fmt.Errorf("currency: the rate provider returned no rate")
	}

	body.Rates[e.base] = 1
	return body.Rates, nil
}

// offered narrows the catalogue to what a configuration asked for, the base
// currency always included: an exchange that cannot quote its own base could
// convert nothing at all.
func offered(base string, wanted []string) []Unit {
	if len(wanted) == 0 {
		units := append([]Unit(nil), catalogue...)
		if !holds(units, base) {
			units = append([]Unit{{Code: base, Symbol: base, Name: base, Decimals: 2}}, units...)
		}
		return units
	}

	keep := map[string]bool{base: true}
	for _, code := range wanted {
		keep[normalise(code)] = true
	}

	units := make([]Unit, 0, len(keep))
	for _, unit := range catalogue {
		if keep[unit.Code] {
			units = append(units, unit)
		}
	}
	if !holds(units, base) {
		units = append([]Unit{{Code: base, Symbol: base, Name: base, Decimals: 2}}, units...)
	}
	return units
}

func holds(units []Unit, code string) bool {
	for _, unit := range units {
		if unit.Code == code {
			return true
		}
	}
	return false
}

func normalise(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

func copyOf(rates map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(rates))
	for code, rate := range rates {
		out[code] = rate
	}
	return out
}
