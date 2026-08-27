// Package billing turns what a caller asks for into calls to Stripe, and turns
// the webhooks Stripe sends back into rows this application can read.
//
// Stripe owns the truth about a subscription. Nothing here decides that someone
// is subscribed: it records what Stripe says, and answers questions from that
// record. A checkout that succeeded is not a subscription until the webhook
// that follows it says so.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"omega/app/models"
	"omega/internal/billing"
	"omega/internal/currency"
)

var (
	ErrPlanUnknown    = errors.New("services: unknown plan")
	ErrPlanUnpriced   = errors.New("services: this plan has no Stripe price configured")
	ErrNoSubscription = errors.New("services: this user has no subscription")
	ErrNoCustomer     = errors.New("services: this user is not a Stripe customer yet")
	ErrWrongMode      = errors.New("services: the event does not match the configured Stripe mode")
)

// Plan is something on sale.
type Plan struct {
	Name      string `mapstructure:"name"`
	Price     string `mapstructure:"price"`
	TrialDays int    `mapstructure:"trial_days"`
}

// Settings is the billing config, as read from config/billing.yaml.
type Settings struct {
	Enabled         bool          `mapstructure:"enabled"`
	SecretKey       string        `mapstructure:"secret_key"`
	WebhookSecret   string        `mapstructure:"webhook_secret"`
	APIVersion      string        `mapstructure:"api_version"`
	BaseURL         string        `mapstructure:"base_url"`
	Timeout         time.Duration `mapstructure:"timeout"`
	MaxAttempts     int           `mapstructure:"max_attempts"`
	Backoff         time.Duration `mapstructure:"backoff"`
	Tolerance       time.Duration `mapstructure:"tolerance"`
	SuccessURL      string        `mapstructure:"success_url"`
	CancelURL       string        `mapstructure:"cancel_url"`
	PortalReturnURL string        `mapstructure:"portal_return_url"`
	Plans           []Plan        `mapstructure:"plans"`
}

// Live reports whether the configured key talks to real money. It is read from
// the key itself rather than from a flag someone could forget to flip.
func (s Settings) Live() bool { return strings.HasPrefix(s.SecretKey, "sk_live_") }

// Plan finds a plan by name.
func (s Settings) Plan(name string) (Plan, bool) {
	for _, plan := range s.Plans {
		if strings.EqualFold(plan.Name, name) {
			return plan, true
		}
	}
	return Plan{}, false
}

// PlanForPrice maps a Stripe price back to the plan it sells. A price that is
// no longer on sale still has subscribers, so an unknown one keeps its id
// rather than being dropped.
func (s Settings) PlanForPrice(price string) string {
	for _, plan := range s.Plans {
		if plan.Price != "" && plan.Price == price {
			return plan.Name
		}
	}
	return price
}

// Service is the billing use cases.
type Service struct {
	db       *gorm.DB
	stripe   *billing.Client
	settings Settings
	currency *currency.Exchange

	priceMu sync.RWMutex
	prices  map[string]heldPrice
}

func NewService(db *gorm.DB, client *billing.Client, settings Settings) *Service {
	return &Service{db: db, stripe: client, settings: settings}
}

// Settings exposes the configuration the routes need.
func (s *Service) Settings() Settings { return s.settings }

// Checkout opens a hosted checkout for a plan and returns the URL to send the
// caller to.
func (s *Service) Checkout(ctx context.Context, user *models.User, planName string) (string, error) {
	plan, found := s.settings.Plan(planName)
	if !found {
		return "", ErrPlanUnknown
	}
	if strings.TrimSpace(plan.Price) == "" {
		return "", ErrPlanUnpriced
	}

	customer, err := s.customer(ctx, user)
	if err != nil {
		return "", err
	}

	// The key is scoped to the user and the plan: a double-clicked button
	// replays the first session instead of opening a second one.
	key := fmt.Sprintf("checkout:%d:%s:%s", user.ID, plan.Name, customer.StripeID)

	// Bill in what the caller reads in, but only where the price declares that
	// currency: Stripe refuses a session naming one it does not.
	bills := s.billsIn(ctx, plan.Price, user.Currency)
	if bills != "" {
		key += ":" + bills
	}

	session, err := s.stripe.CreateCheckoutSession(ctx, billing.CheckoutParams{
		CustomerID: customer.StripeID,
		PriceID:    plan.Price,
		SuccessURL: s.settings.SuccessURL,
		CancelURL:  s.settings.CancelURL,
		TrialDays:  plan.TrialDays,
		Currency:   bills,
		Metadata:   map[string]string{"user_id": user.Key(), "plan": plan.Name},
	}, key)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

// Portal opens Stripe's billing portal for a user who already has a customer.
func (s *Service) Portal(ctx context.Context, user *models.User) (string, error) {
	var customer models.BillingCustomer
	err := s.db.WithContext(ctx).Where("user_id = ?", user.ID).Take(&customer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNoCustomer
	}
	if err != nil {
		return "", err
	}

	// The portal session is short-lived, so every call opens a new one; the key
	// only collapses the duplicates of one click.
	key := fmt.Sprintf("portal:%d:%d", user.ID, time.Now().Unix())

	session, err := s.stripe.CreatePortalSession(ctx, customer.StripeID, s.settings.PortalReturnURL, key)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

// Current returns the subscription a user is entitled to, or nil. A user who
// resubscribed after cancelling has several rows; the live one wins, and
// otherwise the most recent.
func (s *Service) Current(ctx context.Context, userID uint) (*models.BillingSubscription, error) {
	var subscriptions []models.BillingSubscription
	err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&subscriptions).Error
	if err != nil {
		return nil, err
	}

	for i := range subscriptions {
		if subscriptions[i].Active() {
			return &subscriptions[i], nil
		}
	}
	if len(subscriptions) > 0 {
		return &subscriptions[0], nil
	}
	return nil, nil
}

// Subscribed answers the question the rest of an application actually asks.
func (s *Service) Subscribed(ctx context.Context, userID uint) (bool, error) {
	subscription, err := s.Current(ctx, userID)
	if err != nil || subscription == nil {
		return false, err
	}
	return subscription.Active(), nil
}

// Cancel stops a user's subscription, at the end of the paid period unless
// immediately is true. Stripe answers with the new state, and the webhook that
// follows writes it — but the local row is updated now, so the caller does not
// read a stale answer on the next request.
func (s *Service) Cancel(ctx context.Context, userID uint, immediately bool) (*models.BillingSubscription, error) {
	subscription, err := s.Current(ctx, userID)
	if err != nil {
		return nil, err
	}
	if subscription == nil || !subscription.Active() {
		return nil, ErrNoSubscription
	}

	key := fmt.Sprintf("cancel:%s:%t", subscription.StripeID, immediately)
	updated, err := s.stripe.CancelSubscription(ctx, subscription.StripeID, immediately, key)
	if err != nil {
		return nil, err
	}

	if err := s.apply(ctx, s.db, userID, updated); err != nil {
		return nil, err
	}
	return s.Current(ctx, userID)
}

// Resume undoes a cancellation that has not taken effect yet.
func (s *Service) Resume(ctx context.Context, userID uint) (*models.BillingSubscription, error) {
	subscription, err := s.Current(ctx, userID)
	if err != nil {
		return nil, err
	}
	if subscription == nil || !subscription.CancelAtPeriodEnd {
		return nil, ErrNoSubscription
	}

	updated, err := s.stripe.ResumeSubscription(ctx, subscription.StripeID, "resume:"+subscription.StripeID)
	if err != nil {
		return nil, err
	}

	if err := s.apply(ctx, s.db, userID, updated); err != nil {
		return nil, err
	}
	return s.Current(ctx, userID)
}

// customer returns the user's Stripe customer, creating it the first time.
func (s *Service) customer(ctx context.Context, user *models.User) (models.BillingCustomer, error) {
	var existing models.BillingCustomer
	err := s.db.WithContext(ctx).Where("user_id = ?", user.ID).Take(&existing).Error
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.BillingCustomer{}, err
	}

	created, err := s.stripe.CreateCustomer(ctx, user.Email, user.Name, user.Key(), "customer:"+user.Key())
	if err != nil {
		return models.BillingCustomer{}, err
	}

	row := models.BillingCustomer{UserID: user.ID, StripeID: created.ID}

	// Two requests can race here. The unique index on user_id decides, and the
	// loser reads the winner's row rather than creating a second customer.
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	if err != nil {
		return models.BillingCustomer{}, err
	}
	if row.ID == 0 {
		if err := s.db.WithContext(ctx).Where("user_id = ?", user.ID).Take(&existing).Error; err != nil {
			return models.BillingCustomer{}, err
		}
		return existing, nil
	}
	return row, nil
}
