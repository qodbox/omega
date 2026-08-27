package billing

import (
	"time"

	"omega/app/models"
)

// Subscription is what a caller may see of their own subscription. The Stripe
// customer id stays in, because the portal needs it; the price id does not,
// because nothing outside the server has any use for it.
type Subscription struct {
	Status            string     `json:"status"`
	Plan              string     `json:"plan"`
	Active            bool       `json:"active"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	EndedAt           *time.Time `json:"ended_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// NewSubscription presents a subscription, or nil when the user has none.
func NewSubscription(subscription *models.BillingSubscription) *Subscription {
	if subscription == nil {
		return nil
	}
	return &Subscription{
		Status:            subscription.Status,
		Plan:              subscription.Plan,
		Active:            subscription.Active(),
		CancelAtPeriodEnd: subscription.CancelAtPeriodEnd,
		CurrentPeriodEnd:  subscription.CurrentPeriodEnd,
		EndedAt:           subscription.EndedAt,
		CreatedAt:         subscription.CreatedAt,
		UpdatedAt:         subscription.UpdatedAt,
	}
}

// Redirect is a hosted Stripe page the caller must be sent to.
type Redirect struct {
	URL string `json:"url"`
}
