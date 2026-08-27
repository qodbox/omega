package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/billing"
	"omega/internal/currency"
)

func service(t *testing.T) *Service {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.BillingCustomer{},
		&models.BillingSubscription{}, &models.BillingEvent{},
	); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	return NewService(db, billing.New(billing.Config{}), Settings{
		SecretKey: "sk_test_x",
		Plans:     []Plan{{Name: "pro", Price: "price_pro"}},
	})
}

// event builds a signed-and-verified-looking event, as VerifyWebhook would
// have returned it.
func event(id, kind, subscription, customer, status string, extra string) billing.Event {
	object := fmt.Sprintf(
		`{"id":%q,"customer":%q,"status":%q,"current_period_end":1735689600,`+
			`"items":{"data":[{"price":{"id":"price_pro"}}]}%s}`,
		subscription, customer, status, extra)

	return billing.Event{ID: id, Type: kind, Data: json.RawMessage(object)}
}

func customerFor(t *testing.T, s *Service, userID uint, stripeID string) {
	t.Helper()
	if err := s.db.Create(&models.BillingCustomer{UserID: userID, StripeID: stripeID}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAnEventWritesTheSubscriptionAndItsPlan(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	err := s.Handle(ctx, event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", ""))
	if err != nil {
		t.Fatal(err)
	}

	current, err := s.Current(ctx, 7)
	if err != nil || current == nil {
		t.Fatalf("no subscription was written: %v", err)
	}
	if current.Plan != "pro" {
		t.Fatalf("Plan = %q: the price was not mapped back to a plan", current.Plan)
	}
	if !current.Active() {
		t.Fatalf("Status = %q, the subscription should entitle its owner", current.Status)
	}

	subscribed, err := s.Subscribed(ctx, 7)
	if err != nil || !subscribed {
		t.Fatalf("Subscribed = %t (%v)", subscribed, err)
	}
}

func TestTheSameEventTwiceChangesNothingTheSecondTime(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	first := event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", "")
	if err := s.Handle(ctx, first); err != nil {
		t.Fatal(err)
	}

	// Stripe redelivers: same event id, and a payload that would otherwise
	// cancel the subscription.
	replay := event("evt_1", "customer.subscription.deleted", "sub_1", "cus_1", "canceled", "")
	if err := s.Handle(ctx, replay); !errors.Is(err, ErrAlreadyHandled) {
		t.Fatalf("err = %v, want ErrAlreadyHandled", err)
	}

	current, _ := s.Current(ctx, 7)
	if current == nil || current.Status != "active" {
		t.Fatalf("the replay was applied: %+v", current)
	}
}

func TestEventsOutOfOrderKeepOneRowPerSubscription(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	// The update lands before the creation, which Stripe does not promise not
	// to do.
	if err := s.Handle(ctx, event("evt_2", "customer.subscription.updated", "sub_1", "cus_1", "past_due", "")); err != nil {
		t.Fatal(err)
	}
	if err := s.Handle(ctx, event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", "")); err != nil {
		t.Fatal(err)
	}

	var rows int64
	if err := s.db.Model(&models.BillingSubscription{}).Where("stripe_id = ?", "sub_1").Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want a single row for one subscription", rows)
	}
}

func TestACancellationEndsTheEntitlement(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	if err := s.Handle(ctx, event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", "")); err != nil {
		t.Fatal(err)
	}
	if err := s.Handle(ctx, event("evt_2", "customer.subscription.deleted", "sub_1", "cus_1", "canceled", "")); err != nil {
		t.Fatal(err)
	}

	subscribed, err := s.Subscribed(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if subscribed {
		t.Fatal("a cancelled subscription still entitles its owner")
	}
}

func TestAnUnknownCustomerIsResolvedFromTheMetadata(t *testing.T) {
	s := service(t)
	ctx := context.Background()

	// Nothing links cus_9 to a user yet: the subscription names its owner.
	withOwner := event("evt_1", "customer.subscription.created", "sub_9", "cus_9", "active",
		`,"metadata":{"user_id":"42"}`)

	if err := s.Handle(ctx, withOwner); err != nil {
		t.Fatal(err)
	}

	current, err := s.Current(ctx, 42)
	if err != nil || current == nil {
		t.Fatalf("the owner was not resolved: %v", err)
	}

	// And the link is recorded, so the next event needs no metadata.
	var link models.BillingCustomer
	if err := s.db.Where("stripe_id = ?", "cus_9").Take(&link).Error; err != nil {
		t.Fatalf("the customer link was not written: %v", err)
	}
	if link.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", link.UserID)
	}
}

func TestAnEventNobodyOwnsIsNotMarkedHandled(t *testing.T) {
	s := service(t)
	ctx := context.Background()

	// No customer row, no metadata: this cannot be applied.
	orphan := event("evt_1", "customer.subscription.created", "sub_1", "cus_unknown", "active", "")

	if err := s.Handle(ctx, orphan); err == nil {
		t.Fatal("an event with no owner was accepted")
	}

	// The whole thing must have rolled back, or Stripe's retry would be
	// swallowed as an already-handled event and the subscription lost.
	var ledger int64
	if err := s.db.Model(&models.BillingEvent{}).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 0 {
		t.Fatalf("the event was recorded as handled despite failing (%d rows)", ledger)
	}
}

func TestATestEventIsRefusedAgainstLiveKeys(t *testing.T) {
	s := service(t)
	s.settings.SecretKey = "sk_live_x"
	customerFor(t, s, 7, "cus_1")

	testEvent := event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", "")
	testEvent.Livemode = false

	if err := s.Handle(context.Background(), testEvent); !errors.Is(err, ErrWrongMode) {
		t.Fatalf("err = %v, want ErrWrongMode", err)
	}
}

func TestAnEventWeDoNotActOnIsStillAcknowledged(t *testing.T) {
	s := service(t)
	ctx := context.Background()

	unrelated := billing.Event{ID: "evt_1", Type: "invoice.upcoming", Data: json.RawMessage(`{}`)}
	if err := s.Handle(ctx, unrelated); err != nil {
		t.Fatalf("an event we ignore made the endpoint fail: %v", err)
	}

	// Acknowledged means recorded: Stripe must not send it again.
	if err := s.Handle(ctx, unrelated); !errors.Is(err, ErrAlreadyHandled) {
		t.Fatalf("err = %v, want ErrAlreadyHandled", err)
	}
}

func TestAPlanIsFoundByNameWhateverTheCase(t *testing.T) {
	s := service(t)

	if _, found := s.settings.Plan("PRO"); !found {
		t.Fatal("a plan should be found whatever the case")
	}
	if _, found := s.settings.Plan("gold"); found {
		t.Fatal("a plan that is not on sale was found")
	}
	if got := s.settings.PlanForPrice("price_unknown"); got != "price_unknown" {
		t.Fatalf("a retired price should keep its id, got %q", got)
	}
}

func TestCheckoutRefusesWhatIsNotOnSale(t *testing.T) {
	s := service(t)
	user := &models.User{Name: "A", Email: "a@b.c"}

	if _, err := s.Checkout(context.Background(), user, "gold"); !errors.Is(err, ErrPlanUnknown) {
		t.Fatalf("err = %v, want ErrPlanUnknown", err)
	}

	s.settings.Plans = []Plan{{Name: "pro", Price: ""}}
	if _, err := s.Checkout(context.Background(), user, "pro"); !errors.Is(err, ErrPlanUnpriced) {
		t.Fatalf("err = %v, want ErrPlanUnpriced", err)
	}
}

// serviceTalkingTo builds a service whose Stripe client points at a local
// server, so the paths that really call Stripe can be exercised.
func serviceTalkingTo(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	s := service(t)
	s.stripe = billing.New(billing.Config{SecretKey: "sk_test_x", BaseURL: server.URL})
	s.settings.SuccessURL = "https://app.test/done"
	s.settings.CancelURL = "https://app.test/billing"
	s.settings.PortalReturnURL = "https://app.test/billing"
	return s
}

func TestCheckoutCreatesTheCustomerOnceAndAsksForTheRightPrice(t *testing.T) {
	var checkout *http.Request
	customers := 0

	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/v1/customers":
			customers++
			_, _ = w.Write([]byte(`{"id":"cus_1","email":"a@b.c"}`))
		case "/v1/checkout/sessions":
			checkout = r
			_, _ = w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/cs_1"}`))
		case "/v1/prices/price_pro":
			_, _ = w.Write([]byte(`{"id":"price_pro","currency":"eur","unit_amount":1900,
				"recurring":{"interval":"month","interval_count":1}}`))
		default:
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
	})

	user := &models.User{Name: "Ada", Email: "a@b.c"}
	if err := s.db.Create(user).Error; err != nil {
		t.Fatal(err)
	}

	url, err := s.Checkout(context.Background(), user, "pro")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://checkout.stripe.com/cs_1" {
		t.Fatalf("url = %q", url)
	}
	if got := checkout.PostForm.Get("line_items[0][price]"); got != "price_pro" {
		t.Fatalf("price = %q, the plan was not resolved to its price", got)
	}
	if got := checkout.PostForm.Get("subscription_data[metadata][user_id]"); got != user.Key() {
		t.Fatalf("metadata[user_id] = %q: the webhook could not trace the owner", got)
	}

	// A second checkout reuses the customer rather than creating another.
	if _, err := s.Checkout(context.Background(), user, "pro"); err != nil {
		t.Fatal(err)
	}
	if customers != 1 {
		t.Fatalf("the customer was created %d times", customers)
	}
}

func TestThePortalNeedsACustomerFirst(t *testing.T) {
	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"https://billing.stripe.com/p_1"}`))
	})

	user := &models.User{Name: "Ada", Email: "a@b.c"}
	if err := s.db.Create(user).Error; err != nil {
		t.Fatal(err)
	}

	// Nothing has ever been billed to this account.
	if _, err := s.Portal(context.Background(), user); !errors.Is(err, ErrNoCustomer) {
		t.Fatalf("err = %v, want ErrNoCustomer", err)
	}

	customerFor(t, s, user.ID, "cus_1")

	url, err := s.Portal(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://billing.stripe.com/p_1" {
		t.Fatalf("url = %q", url)
	}
}

func TestCancellingWritesStripesAnswerLocally(t *testing.T) {
	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"sub_1","customer":"cus_1","status":"active",` +
			`"cancel_at_period_end":true,"current_period_end":1735689600,` +
			`"items":{"data":[{"price":{"id":"price_pro"}}]}}`))
	})
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	if err := s.Handle(ctx, event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", "")); err != nil {
		t.Fatal(err)
	}

	updated, err := s.Cancel(ctx, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.CancelAtPeriodEnd {
		t.Fatal("the local row still says the subscription renews")
	}
	// It runs to the end of the period already paid for.
	if !updated.Active() {
		t.Fatalf("Status = %q: access was cut before the period ended", updated.Status)
	}
}

func TestThereIsNothingToCancelWithoutASubscription(t *testing.T) {
	s := service(t)

	if _, err := s.Cancel(context.Background(), 7, false); !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("err = %v, want ErrNoSubscription", err)
	}
	if _, err := s.Resume(context.Background(), 7); !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("err = %v, want ErrNoSubscription", err)
	}
}

// exchangeQuoting builds an exchange whose rate provider is local, so the
// conversions below are exact rather than whatever the day brings.
func exchangeQuoting(t *testing.T, rates string) *currency.Exchange {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rates))
	}))
	t.Cleanup(server.Close)

	return currency.New(currency.Config{Base: "EUR", Endpoint: server.URL})
}

func TestAPlanStripePricesInTheCallersCurrencyIsQuotedExactly(t *testing.T) {
	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		// A multi-currency price: Stripe will really bill 2100 in USD.
		_, _ = w.Write([]byte(`{"id":"price_pro","currency":"eur","unit_amount":1900,
			"recurring":{"interval":"month"},
			"currency_options":{"usd":{"unit_amount":2100}}}`))
	})
	s.WithCurrency(exchangeQuoting(t, `{"rates":{"USD":1.5}}`))

	offers, err := s.Plans(context.Background(), "USD")
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 {
		t.Fatalf("offers = %d, want 1", len(offers))
	}

	offer := offers[0]
	if !offer.Charged {
		t.Fatal("a price Stripe declares in USD should be marked as charged in USD")
	}
	if offer.Amount != 2100 {
		t.Fatalf("Amount = %d, want Stripe's own 2100 — not a conversion of 1900", offer.Amount)
	}
	if offer.Currency != "USD" || offer.Settles != "USD" {
		t.Fatalf("Currency = %q, Settles = %q", offer.Currency, offer.Settles)
	}
	if offer.Interval != "month" {
		t.Fatalf("Interval = %q", offer.Interval)
	}
}

func TestAPlanStripeDoesNotPriceIsConvertedAndSaidSo(t *testing.T) {
	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		// Single-currency: euros, and nothing else.
		_, _ = w.Write([]byte(`{"id":"price_pro","currency":"eur","unit_amount":1900,
			"recurring":{"interval":"month"}}`))
	})
	s.WithCurrency(exchangeQuoting(t, `{"rates":{"USD":1.5}}`))

	offers, err := s.Plans(context.Background(), "USD")
	if err != nil {
		t.Fatal(err)
	}

	offer := offers[0]
	if offer.Charged {
		t.Fatal("a converted amount must not claim Stripe charges in that currency")
	}
	if offer.Amount != 2850 { // 1900 * 1.5
		t.Fatalf("Amount = %d, want 2850", offer.Amount)
	}
	if offer.Currency != "USD" {
		t.Fatalf("Currency = %q, the caller reads in USD", offer.Currency)
	}
	if offer.Settles != "EUR" {
		t.Fatalf("Settles = %q: the customer must be told the card is charged in euros", offer.Settles)
	}
}

func TestAnUnknownCurrencyFallsBackToTheBase(t *testing.T) {
	s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"price_pro","currency":"eur","unit_amount":1900,
			"recurring":{"interval":"month"}}`))
	})
	s.WithCurrency(exchangeQuoting(t, `{"rates":{"USD":1.5}}`))

	offers, err := s.Plans(context.Background(), "WAT")
	if err != nil {
		t.Fatal(err)
	}
	if offers[0].Currency != "EUR" || offers[0].Amount != 1900 {
		t.Fatalf("offer = %+v, a currency nobody quotes must fall back to the base", offers[0])
	}
}

func TestCheckoutNamesACurrencyOnlyWhenThePriceDeclaresIt(t *testing.T) {
	for name, tc := range map[string]struct {
		price string
		reads string
		want  string
	}{
		"multi-currency price": {
			price: `{"id":"price_pro","currency":"eur","unit_amount":1900,"currency_options":{"usd":{"unit_amount":2100}}}`,
			reads: "USD",
			want:  "usd",
		},
		"single-currency price": {
			price: `{"id":"price_pro","currency":"eur","unit_amount":1900}`,
			reads: "USD",
			want:  "", // Stripe would refuse a session naming a currency it has no amount for.
		},
		"already the price's own currency": {
			price: `{"id":"price_pro","currency":"eur","unit_amount":1900}`,
			reads: "EUR",
			want:  "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var checkout *http.Request

			s := serviceTalkingTo(t, func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				switch r.URL.Path {
				case "/v1/customers":
					_, _ = w.Write([]byte(`{"id":"cus_1"}`))
				case "/v1/prices/price_pro":
					_, _ = w.Write([]byte(tc.price))
				case "/v1/checkout/sessions":
					checkout = r
					_, _ = w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/cs_1"}`))
				}
			})
			s.WithCurrency(exchangeQuoting(t, `{"rates":{"USD":1.5}}`))

			user := &models.User{Name: "Ada", Email: "a@b.c", Currency: tc.reads}
			if err := s.db.Create(user).Error; err != nil {
				t.Fatal(err)
			}

			if _, err := s.Checkout(context.Background(), user, "pro"); err != nil {
				t.Fatal(err)
			}
			if got := checkout.PostForm.Get("currency"); got != tc.want {
				t.Fatalf("currency = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheWebhookRecordsWhatStripeBilledIn(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	customerFor(t, s, 7, "cus_1")

	billed := event("evt_1", "customer.subscription.created", "sub_1", "cus_1", "active", `,"currency":"usd"`)
	if err := s.Handle(ctx, billed); err != nil {
		t.Fatal(err)
	}

	current, _ := s.Current(ctx, 7)
	if current == nil || current.Currency != "USD" {
		t.Fatalf("Currency = %q, want the currency Stripe charged", current.Currency)
	}
}
