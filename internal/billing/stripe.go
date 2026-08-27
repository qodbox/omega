package billing

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Customer is a Stripe customer, the object a subscription hangs from.
type Customer struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Session is a hosted page Stripe opens for the caller: a checkout, or the
// billing portal. Only its URL matters to us.
type Session struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Subscription is the state Stripe holds. It is the source of truth; the row in
// our database is a copy kept in step by the webhooks.
type Subscription struct {
	ID                string `json:"id"`
	CustomerID        string `json:"customer"`
	Status            string `json:"status"`
	Currency          string `json:"currency"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
	CurrentPeriodEnd  int64  `json:"current_period_end"`
	EndedAt           int64  `json:"ended_at"`

	Items struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

// PriceID is the price the subscription is billed on. A subscription can carry
// several items; the first one is the plan in the single-price model this layer
// supports, and an application that sells add-ons reads Items itself.
func (s Subscription) PriceID() string {
	if len(s.Items.Data) == 0 {
		return ""
	}
	return s.Items.Data[0].Price.ID
}

// PeriodEnd is when the current period closes, or nil if Stripe sent none.
func (s Subscription) PeriodEnd() *time.Time { return unixOrNil(s.CurrentPeriodEnd) }

// Ended is when the subscription stopped, or nil while it is still running.
func (s Subscription) Ended() *time.Time { return unixOrNil(s.EndedAt) }

func unixOrNil(seconds int64) *time.Time {
	if seconds <= 0 {
		return nil
	}
	at := time.Unix(seconds, 0).UTC()
	return &at
}

// Active reports whether the subscription entitles its owner to the plan.
// A subscription past due is deliberately still active: Stripe retries the
// payment for days, and cutting access on the first failed charge punishes a
// customer whose card simply expired.
func Active(status string) bool {
	switch status {
	case "active", "trialing", "past_due":
		return true
	default:
		return false
	}
}

// CreateCustomer registers a customer and ties it to a caller of ours. The key
// travels in the metadata so a Stripe dashboard shows who a customer is, and so
// a webhook that carries only a customer id can be traced back.
func (c *Client) CreateCustomer(ctx context.Context, email, name, key, idempotencyKey string) (Customer, error) {
	form := url.Values{}
	form.Set("email", email)
	if name != "" {
		form.Set("name", name)
	}
	if key != "" {
		form.Set("metadata[user_id]", key)
	}

	var customer Customer
	err := c.post(ctx, "/v1/customers", form, idempotencyKey, &customer)
	return customer, err
}

// GetCustomer reads a customer back.
func (c *Client) GetCustomer(ctx context.Context, id string) (Customer, error) {
	var customer Customer
	err := c.get(ctx, "/v1/customers/"+url.PathEscape(id), &customer)
	return customer, err
}

// Price is what a plan costs, as Stripe holds it.
type Price struct {
	ID         string `json:"id"`
	Currency   string `json:"currency"`
	UnitAmount int64  `json:"unit_amount"`

	Recurring struct {
		Interval      string `json:"interval"`
		IntervalCount int    `json:"interval_count"`
	} `json:"recurring"`

	// CurrencyOptions holds the amounts Stripe will really charge in another
	// currency. It only arrives when the request expands it, and it is empty
	// unless the price was set up as multi-currency.
	CurrencyOptions map[string]struct {
		UnitAmount int64 `json:"unit_amount"`
	} `json:"currency_options"`
}

// AmountIn reports what Stripe would charge in one currency, and whether it
// would charge in it at all. A currency the price does not declare is not a
// price: converting it would be quoting a figure nobody will be billed.
func (p Price) AmountIn(code string) (int64, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == strings.ToLower(p.Currency) {
		return p.UnitAmount, true
	}
	option, found := p.CurrencyOptions[code]
	if !found {
		return 0, false
	}
	return option.UnitAmount, true
}

// GetPrice reads a price, with the multi-currency amounts expanded: without
// that expansion Stripe leaves currency_options out, and every price looks
// single-currency.
func (c *Client) GetPrice(ctx context.Context, id string) (Price, error) {
	var price Price
	err := c.get(ctx, "/v1/prices/"+url.PathEscape(id)+"?expand[]=currency_options", &price)
	return price, err
}

// CheckoutParams is what opening a subscription checkout needs.
type CheckoutParams struct {
	CustomerID string
	PriceID    string
	SuccessURL string
	CancelURL  string
	TrialDays  int

	// Currency asks Stripe to bill in something other than the price's own
	// currency. Send it only for a price that declares that currency: Stripe
	// refuses the session otherwise.
	Currency string

	// Metadata is copied onto the subscription Stripe creates, which is how the
	// webhook that follows knows which plan was bought.
	Metadata map[string]string
}

// CreateCheckoutSession opens a hosted checkout for a subscription.
func (c *Client) CreateCheckoutSession(ctx context.Context, params CheckoutParams, idempotencyKey string) (Session, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("customer", params.CustomerID)
	form.Set("success_url", params.SuccessURL)
	form.Set("cancel_url", params.CancelURL)
	form.Set("line_items[0][price]", params.PriceID)
	form.Set("line_items[0][quantity]", "1")

	if params.Currency != "" {
		form.Set("currency", strings.ToLower(params.Currency))
	}
	if params.TrialDays > 0 {
		form.Set("subscription_data[trial_period_days]", strconv.Itoa(params.TrialDays))
	}
	for key, value := range params.Metadata {
		form.Set("subscription_data[metadata]["+key+"]", value)
		form.Set("metadata["+key+"]", value)
	}

	var session Session
	err := c.post(ctx, "/v1/checkout/sessions", form, idempotencyKey, &session)
	return session, err
}

// CreatePortalSession opens Stripe's billing portal, where a customer changes
// their card, reads their invoices or cancels — none of which we reimplement.
func (c *Client) CreatePortalSession(ctx context.Context, customerID, returnURL, idempotencyKey string) (Session, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", returnURL)

	var session Session
	err := c.post(ctx, "/v1/billing_portal/sessions", form, idempotencyKey, &session)
	return session, err
}

// GetSubscription reads a subscription from Stripe.
func (c *Client) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	var subscription Subscription
	err := c.get(ctx, "/v1/subscriptions/"+url.PathEscape(id), &subscription)
	return subscription, err
}

// CancelSubscription stops a subscription: at the end of the paid period by
// default, which is what a customer expects, or immediately when now is true.
func (c *Client) CancelSubscription(ctx context.Context, id string, now bool, idempotencyKey string) (Subscription, error) {
	var subscription Subscription

	if now {
		err := c.post(ctx, "/v1/subscriptions/"+url.PathEscape(id)+"/cancel", url.Values{}, idempotencyKey, &subscription)
		return subscription, err
	}

	form := url.Values{}
	form.Set("cancel_at_period_end", "true")
	err := c.post(ctx, "/v1/subscriptions/"+url.PathEscape(id), form, idempotencyKey, &subscription)
	return subscription, err
}

// ResumeSubscription undoes a cancellation that has not taken effect yet.
func (c *Client) ResumeSubscription(ctx context.Context, id, idempotencyKey string) (Subscription, error) {
	form := url.Values{}
	form.Set("cancel_at_period_end", "false")

	var subscription Subscription
	err := c.post(ctx, "/v1/subscriptions/"+url.PathEscape(id), form, idempotencyKey, &subscription)
	return subscription, err
}
