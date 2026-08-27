package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"omega/app/models"
	"omega/internal/billing"
)

// ErrAlreadyHandled reports an event this application has already applied.
// It is not a failure: Stripe retries an event until it is acknowledged, so
// seeing one twice is the normal case, and the caller answers 200.
var ErrAlreadyHandled = errors.New("services: this event was already handled")

// handled lists what changes a subscription. Everything else is acknowledged
// and dropped: a webhook endpoint that fails on an event it does not know
// makes Stripe retry it for days.
var handled = map[string]bool{
	"customer.subscription.created": true,
	"customer.subscription.updated": true,
	"customer.subscription.deleted": true,
	"customer.subscription.paused":  true,
	"customer.subscription.resumed": true,
}

// Handle applies a verified Stripe event.
//
// The whole thing runs in one transaction, and the event id is inserted in it:
// either the ledger row and the change it describes both land, or neither does
// and Stripe's next retry tries again. Recording the event outside the
// transaction would turn a transient database failure into an event that is
// marked handled and never was.
func (s *Service) Handle(ctx context.Context, event billing.Event) error {
	if event.Livemode != s.settings.Live() {
		// A test event against live keys — or the reverse — means the webhook
		// is wired to the wrong endpoint. Applying it would hand out a paid
		// plan for a payment that never happened.
		return ErrWrongMode
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ledger := models.BillingEvent{ID: event.ID, Type: event.Type, ProcessedAt: time.Now().UTC()}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ledger)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrAlreadyHandled
		}

		if !handled[event.Type] {
			return nil
		}

		var subscription billing.Subscription
		if err := json.Unmarshal(event.Data, &subscription); err != nil {
			return fmt.Errorf("services: decoding the subscription in %s: %w", event.Type, err)
		}
		if subscription.ID == "" {
			return fmt.Errorf("services: %s carried no subscription", event.Type)
		}

		userID, err := s.owner(tx, event.Data, subscription.CustomerID)
		if err != nil {
			return err
		}

		if event.Type == "customer.subscription.deleted" && subscription.Status == "" {
			subscription.Status = "canceled"
		}
		return s.apply(ctx, tx, userID, subscription)
	})
}

// owner finds which of our users a subscription belongs to. The customer id is
// the reliable link; the metadata is a fallback for a subscription created
// outside this application, from the Stripe dashboard.
func (s *Service) owner(tx *gorm.DB, raw json.RawMessage, customerID string) (uint, error) {
	var customer models.BillingCustomer
	err := tx.Where("stripe_id = ?", customerID).Take(&customer).Error
	if err == nil {
		return customer.UserID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	var envelope struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Metadata.UserID != "" {
		id, convErr := strconv.ParseUint(envelope.Metadata.UserID, 10, 64)
		if convErr == nil && id > 0 {
			// The customer is unknown to us, but the subscription names its
			// owner: record the link so the next event resolves directly.
			if customerID != "" {
				link := models.BillingCustomer{UserID: uint(id), StripeID: customerID}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
					return 0, err
				}
			}
			return uint(id), nil
		}
	}

	return 0, fmt.Errorf("services: no user matches Stripe customer %q", customerID)
}

// apply writes Stripe's state onto the local row, creating it if this is the
// first time we hear about the subscription.
func (s *Service) apply(ctx context.Context, tx *gorm.DB, userID uint, remote billing.Subscription) error {
	row := models.BillingSubscription{
		UserID:            userID,
		StripeID:          remote.ID,
		CustomerID:        remote.CustomerID,
		Status:            remote.Status,
		PriceID:           remote.PriceID(),
		Plan:              s.settings.PlanForPrice(remote.PriceID()),
		Currency:          strings.ToUpper(remote.Currency),
		CancelAtPeriodEnd: remote.CancelAtPeriodEnd,
		CurrentPeriodEnd:  remote.PeriodEnd(),
		EndedAt:           remote.Ended(),
	}

	// Stripe delivers events out of order often enough to matter. Upserting on
	// the subscription id keeps one row per subscription whichever event lands
	// first, and the columns listed are exactly the ones Stripe owns.
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stripe_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"user_id", "customer_id", "status", "price_id", "plan",
			"cancel_at_period_end", "current_period_end", "ended_at", "updated_at",
		}),
	}).Create(&row).Error
}
