package currency

import (
	"context"

	"omega/internal/currency"
)

// Catalogue is everything a client needs to display amounts itself: the
// currency the figures are stored in, what may be chosen, and the day's rates.
type Catalogue struct {
	Base       string             `json:"base"`
	Currencies []currency.Unit    `json:"currencies"`
	Rates      map[string]float64 `json:"rates"`
}

// NewCatalogue reads the exchange once, so every client is served the same
// table rather than each querying a central bank of its own.
func NewCatalogue(ctx context.Context, exchange *currency.Exchange) Catalogue {
	return Catalogue{
		Base:       exchange.Base(),
		Currencies: exchange.Units(),
		Rates:      exchange.Rates(ctx),
	}
}

// Choice is the preference as it now stands.
type Choice struct {
	Currency string  `json:"currency"`
	Base     string  `json:"base"`
	Rate     float64 `json:"rate"`
}
