package models

import "time"

// BillingCustomer ties a user of ours to a customer of Stripe's. One row per
// user: creating a second customer for the same person splits their invoices
// and their payment methods across two accounts, which nothing later can merge.
type BillingCustomer struct {
	ID       uint   `gorm:"primarykey" json:"id"`
	UserID   uint   `gorm:"not null;uniqueIndex" json:"user_id"`
	StripeID string `gorm:"size:64;not null;uniqueIndex" json:"stripe_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (BillingCustomer) TableName() string { return "billing_customers" }

// BillingSubscription mirrors a Stripe subscription. Stripe owns the truth;
// this row is a local copy the webhooks keep in step, so that answering "may
// this user do that?" costs a query here instead of a call to Stripe.
type BillingSubscription struct {
	ID         uint   `gorm:"primarykey" json:"id"`
	UserID     uint   `gorm:"not null;index" json:"user_id"`
	StripeID   string `gorm:"size:64;not null;uniqueIndex" json:"stripe_id"`
	CustomerID string `gorm:"size:64;not null;index" json:"customer_id"`

	Status  string `gorm:"size:32;not null;index" json:"status"`
	PriceID string `gorm:"size:64;not null" json:"price_id"`
	Plan    string `gorm:"size:64;not null;index" json:"plan"`

	CancelAtPeriodEnd bool       `gorm:"not null;default:false" json:"cancel_at_period_end"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end,omitempty"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (BillingSubscription) TableName() string { return "billing_subscriptions" }

// Active reports whether this subscription entitles its owner to their plan.
func (s *BillingSubscription) Active() bool {
	switch s.Status {
	case "active", "trialing", "past_due":
		return true
	default:
		return false
	}
}

// BillingEvent is the ledger of the Stripe events already applied. Stripe
// guarantees at-least-once delivery and retries for days, so the same event
// arrives more than once; the primary key is Stripe's own event id, and
// inserting it is what makes handling an event happen exactly once.
type BillingEvent struct {
	ID   string `gorm:"size:64;primarykey" json:"id"`
	Type string `gorm:"size:64;not null;index" json:"type"`

	ProcessedAt time.Time `json:"processed_at"`
}

func (BillingEvent) TableName() string { return "billing_events" }
