package billing

import (
	"context"
	"strings"
	"time"

	"omega/internal/billing"
	"omega/internal/currency"
)

// priceTTL is how long a Stripe price is remembered. Prices are immutable, so
// this only bounds how long a *new* one takes to appear.
const priceTTL = 10 * time.Minute

// Offer is a plan as a caller reads it, priced in their own currency.
type Offer struct {
	Name      string `json:"name"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Interval  string `json:"interval"`
	TrialDays int    `json:"trial_days"`

	// Charged says whether Stripe will really bill in Currency. When it is
	// false, Amount is a conversion meant for reading and the card is charged
	// in Settles — the distinction a customer is owed before they click.
	Charged bool   `json:"charged"`
	Settles string `json:"settles"`
}

// WithCurrency gives the service an exchange, which is what lets a plan be read
// in a currency Stripe does not price it in.
func (s *Service) WithCurrency(exchange *currency.Exchange) *Service {
	s.currency = exchange
	return s
}

// Plans returns what is on sale, priced in the currency asked for.
//
// A plan Stripe prices in that currency is quoted exactly. One it does not is
// converted at the day's rate and marked as settling elsewhere: the amount is
// then a guide, and the charge happens in the price's own currency.
func (s *Service) Plans(ctx context.Context, want string) ([]Offer, error) {
	want = s.readsIn(want)
	offers := make([]Offer, 0, len(s.settings.Plans))

	for _, plan := range s.settings.Plans {
		if strings.TrimSpace(plan.Price) == "" {
			continue
		}

		price, err := s.price(ctx, plan.Price)
		if err != nil {
			return nil, err
		}

		offer := Offer{
			Name:      plan.Name,
			Interval:  price.Recurring.Interval,
			TrialDays: plan.TrialDays,
			Currency:  want,
			Settles:   strings.ToUpper(price.Currency),
		}

		if amount, charged := price.AmountIn(want); charged {
			offer.Amount, offer.Charged, offer.Settles = amount, true, want
		} else {
			offer.Amount = s.convert(ctx, price.UnitAmount, price.Currency, want)
		}

		offers = append(offers, offer)
	}
	return offers, nil
}

// billsIn reports the currency Stripe would charge a plan in for this caller,
// and the empty string when that is simply the price's own currency — which is
// what must be sent to Stripe, since naming a currency a price does not declare
// has the session refused.
func (s *Service) billsIn(ctx context.Context, priceID, want string) string {
	want = s.readsIn(want)

	price, err := s.price(ctx, priceID)
	if err != nil {
		// The price could not be read; let Stripe bill in its own currency
		// rather than fail a checkout over a display preference.
		return ""
	}
	if strings.EqualFold(price.Currency, want) {
		return ""
	}
	if _, charged := price.AmountIn(want); charged {
		return want
	}
	return ""
}

// readsIn narrows whatever was asked for to something the exchange can serve.
func (s *Service) readsIn(want string) string {
	if s.currency == nil {
		return strings.ToUpper(strings.TrimSpace(want))
	}
	return s.currency.Normalise(want)
}

func (s *Service) convert(ctx context.Context, minor int64, from, to string) int64 {
	if s.currency == nil {
		return minor
	}
	return s.currency.ConvertFrom(ctx, minor, from, to)
}

// price reads a Stripe price, remembering it for a while: a plan listing would
// otherwise call Stripe once per plan, on every request.
func (s *Service) price(ctx context.Context, id string) (billing.Price, error) {
	s.priceMu.RLock()
	held, found := s.prices[id]
	s.priceMu.RUnlock()

	if found && time.Since(held.at) < priceTTL {
		return held.price, nil
	}

	price, err := s.stripe.GetPrice(ctx, id)
	if err != nil {
		// A stale price beats no listing at all when Stripe is unreachable.
		if found {
			return held.price, nil
		}
		return billing.Price{}, err
	}

	s.priceMu.Lock()
	if s.prices == nil {
		s.prices = map[string]heldPrice{}
	}
	s.prices[id] = heldPrice{price: price, at: time.Now()}
	s.priceMu.Unlock()

	return price, nil
}

type heldPrice struct {
	price billing.Price
	at    time.Time
}
