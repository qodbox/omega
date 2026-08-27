package billing

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"omega/app/models"
	presenter "omega/app/presenters/billing"
	request "omega/app/requests/billing"
	service "omega/app/services/billing"
	jwt "omega/internal/auth"
	"omega/internal/billing"
	"omega/internal/validation"
)

type Controller struct {
	billing *service.Service
	guard   *jwt.Guard
	log     zerolog.Logger
}

func NewController(service *service.Service, guard *jwt.Guard, log zerolog.Logger) *Controller {
	return &Controller{billing: service, guard: guard, log: log}
}

// Checkout answers with the hosted page the caller must be sent to.
func (b *Controller) Checkout(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Checkout](c)
	if err != nil {
		return err
	}

	user, err := b.caller(c)
	if err != nil {
		return err
	}

	url, err := b.billing.Checkout(c.UserContext(), user, body.Plan)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"data": presenter.Redirect{URL: url}})
}

// Portal answers with the billing portal page for the caller.
func (b *Controller) Portal(c *fiber.Ctx) error {
	user, err := b.caller(c)
	if err != nil {
		return err
	}

	url, err := b.billing.Portal(c.UserContext(), user)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"data": presenter.Redirect{URL: url}})
}

// Show answers with the caller's subscription, or a null one.
func (b *Controller) Show(c *fiber.Ctx) error {
	user, err := b.caller(c)
	if err != nil {
		return err
	}

	subscription, err := b.billing.Current(c.UserContext(), user.ID)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"data": presenter.NewSubscription(subscription)})
}

// Cancel stops the caller's subscription.
func (b *Controller) Cancel(c *fiber.Ctx) error {
	user, err := b.caller(c)
	if err != nil {
		return err
	}

	var body request.Cancel
	// The body is optional: cancelling with no body means "at period end".
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Malformed body.")
		}
	}

	subscription, err := b.billing.Cancel(c.UserContext(), user.ID, body.Immediately)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"data": presenter.NewSubscription(subscription)})
}

// Resume undoes a cancellation that has not taken effect yet.
func (b *Controller) Resume(c *fiber.Ctx) error {
	user, err := b.caller(c)
	if err != nil {
		return err
	}

	subscription, err := b.billing.Resume(c.UserContext(), user.ID)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"data": presenter.NewSubscription(subscription)})
}

// Webhook takes what Stripe sends.
//
// It is the only public route here, so it authenticates the request itself,
// from the signature over the raw body. Nothing is read out of the payload
// before that check passes.
func (b *Controller) Webhook(c *fiber.Ctx) error {
	settings := b.billing.Settings()

	event, err := billing.VerifyWebhook(
		c.Body(),
		c.Get("Stripe-Signature"),
		settings.WebhookSecret,
		settings.Tolerance,
	)
	if err != nil {
		// The body is not logged: an unverified payload is attacker-controlled.
		b.log.Warn().Err(err).Str("ip", c.IP()).Msg("rejected a Stripe webhook")
		return fiber.NewError(fiber.StatusBadRequest, "Invalid signature.")
	}

	switch err := b.billing.Handle(c.UserContext(), event); {
	case err == nil:
		b.log.Info().Str("event", event.Type).Str("id", event.ID).Msg("applied a Stripe event")

	case errors.Is(err, service.ErrAlreadyHandled):
		// Stripe retries until it is acknowledged; saying so ends the retries.

	case errors.Is(err, service.ErrWrongMode):
		b.log.Error().Str("event", event.ID).Bool("livemode", event.Livemode).
			Msg("a Stripe event does not match the configured mode — check which endpoint points here")
		return fiber.NewError(fiber.StatusBadRequest, "Wrong Stripe mode.")

	default:
		// Answering 500 is deliberate: Stripe will send this event again, and
		// the transaction rolled back, so the retry starts from a clean state.
		b.log.Error().Err(err).Str("event", event.ID).Str("type", event.Type).
			Msg("failed to apply a Stripe event")
		return err
	}

	return c.JSON(fiber.Map{"received": true})
}

func (b *Controller) caller(c *fiber.Ctx) (*models.User, error) {
	user, ok := b.guard.User(c).(*models.User)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	}
	return user, nil
}

func translate(err error) error {
	switch {
	case errors.Is(err, service.ErrPlanUnknown):
		return fiber.NewError(fiber.StatusUnprocessableEntity, "Unknown plan.")
	case errors.Is(err, service.ErrPlanUnpriced):
		return fiber.NewError(fiber.StatusServiceUnavailable, "This plan is not on sale yet.")
	case errors.Is(err, service.ErrNoSubscription):
		return fiber.NewError(fiber.StatusNotFound, "No subscription to change.")
	case errors.Is(err, service.ErrNoCustomer):
		return fiber.NewError(fiber.StatusNotFound, "Nothing has ever been billed to this account.")
	case errors.Is(err, billing.ErrNotConfigured):
		return fiber.NewError(fiber.StatusServiceUnavailable, "Billing is not configured.")
	default:
		return err
	}
}
