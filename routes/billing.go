package routes

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"omega"
	controller "omega/app/controllers/billing"
	service "omega/app/services/billing"
	"omega/internal/api"
	jwt "omega/internal/auth"
	"omega/internal/billing"
)

// registerBilling wires the Stripe routes, if billing is turned on.
//
// It is deliberately all-or-nothing: an application with billing disabled has
// no billing routes at all, rather than routes that answer 500. What it does
// refuse to do is come up half-configured — a webhook endpoint without its
// signing secret would accept forged requests.
func registerBilling(app *omega.App, group fiber.Router, guard *jwt.Guard, registry *api.Registry) error {
	var settings service.Settings
	if err := app.Cfg.Unmarshal("billing", &settings); err != nil {
		return fmt.Errorf("routes: billing config: %w", err)
	}
	if !settings.Enabled {
		return nil
	}
	if strings.TrimSpace(settings.SecretKey) == "" {
		return fmt.Errorf("routes: billing is enabled but billing.secret_key is empty — set STRIPE_SECRET_KEY")
	}
	if strings.TrimSpace(settings.WebhookSecret) == "" {
		return fmt.Errorf("routes: billing is enabled but billing.webhook_secret is empty — " +
			"without it a forged webhook is indistinguishable from a real one (set STRIPE_WEBHOOK_SECRET)")
	}

	client := billing.New(billing.Config{
		SecretKey:   settings.SecretKey,
		APIVersion:  settings.APIVersion,
		BaseURL:     settings.BaseURL,
		Timeout:     settings.Timeout,
		MaxAttempts: settings.MaxAttempts,
		Backoff:     settings.Backoff,
	})

	billed := service.NewService(app.DB, client, settings).WithCurrency(app.Currency)
	omega.Bind("billing", billed)

	local := controller.NewController(billed, guard, app.Log)
	routes := group.Group("/billing")

	// Public, and authenticated by the Stripe signature rather than by a token:
	// Stripe has no bearer token of ours to send.
	routes.Post("/webhook", local.Webhook).Name("billing.webhook")

	// Public, and priced in the caller's currency when there is a caller.
	routes.Get("/plans", guard.Optional(), local.Plans).Name("billing.plans")

	routes.Get("/subscription", guard.Required(), local.Show).Name("billing.subscription")
	routes.Post("/checkout", guard.Required(), local.Checkout).Name("billing.checkout")
	routes.Post("/portal", guard.Required(), local.Portal).Name("billing.portal")
	routes.Post("/cancel", guard.Required(), local.Cancel).Name("billing.cancel")
	routes.Post("/resume", guard.Required(), local.Resume).Name("billing.resume")

	documentBilling(registry, settings)

	app.Log.Info().
		Bool("livemode", settings.Live()).
		Int("plans", len(settings.Plans)).
		Msg("billing enabled")

	return nil
}

func documentBilling(registry *api.Registry, settings service.Settings) {
	names := make([]string, 0, len(settings.Plans))
	for _, plan := range settings.Plans {
		names = append(names, plan.Name)
	}

	registry.Document("/billing/plans", api.PathItem{
		Get: (&api.Operation{
			Tags:    []string{"billing"},
			Summary: "What is on sale",
			Description: "Each plan priced in the caller's own currency. A plan Stripe prices in that " +
				"currency is quoted exactly and `charged` is true; one it does not is converted at the " +
				"day's rate, `charged` is false, and `settles` names the currency the card is really " +
				"billed in.",
			OperationID: "billing.plans",
			Parameters: []api.Parameter{{
				Name: "currency", In: "query", Required: false,
				Description: "Read the prices in this currency. A signed-in caller's own choice is used otherwise.",
				Schema:      api.String(),
			}},
			Responses: map[string]api.Response{
				"200": api.JSON("The plans on sale", dataOf(&api.Schema{
					Type: "array", Items: api.Object(map[string]*api.Schema{
						"name":       api.String(),
						"amount":     api.Integer(),
						"currency":   api.String(),
						"interval":   api.String(),
						"trial_days": api.Integer(),
						"charged":    {Type: "boolean"},
						"settles":    api.String(),
					}),
				})),
				"503": api.Failure("Billing is not configured"),
			},
		}).Public(),
	})

	registry.Document("/billing/subscription", api.PathItem{
		Get: &api.Operation{
			Tags:        []string{"billing"},
			Summary:     "The caller's subscription",
			Description: "Answers with the subscription, or null when the caller has never subscribed.",
			OperationID: "billing.subscription",
			Responses: map[string]api.Response{
				"200": api.JSON("The subscription", dataOf(subscriptionSchema())),
				"401": api.Failure("Authentication required"),
			},
		},
	})

	registry.Document("/billing/checkout", api.PathItem{
		Post: &api.Operation{
			Tags:    []string{"billing"},
			Summary: "Open a checkout",
			Description: "Answers with the URL of a hosted Stripe page. A plan is named, never priced: " +
				"the price ids stay on the server. Subscribing is not effective until the webhook " +
				"that follows the payment says so.",
			OperationID: "billing.checkout",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"plan": {Type: "string", Enum: names},
			}, "plan")),
			Responses: map[string]api.Response{
				"200": api.JSON("The page to send the caller to", dataOf(redirectSchema())),
				"401": api.Failure("Authentication required"),
				"422": api.Failure("Unknown plan"),
				"503": api.Failure("Billing is not configured"),
			},
		},
	})

	registry.Document("/billing/portal", api.PathItem{
		Post: &api.Operation{
			Tags:        []string{"billing"},
			Summary:     "Open the billing portal",
			Description: "Answers with the URL of Stripe's portal, where the caller changes their card, reads their invoices or cancels.",
			OperationID: "billing.portal",
			Responses: map[string]api.Response{
				"200": api.JSON("The page to send the caller to", dataOf(redirectSchema())),
				"401": api.Failure("Authentication required"),
				"404": api.Failure("Nothing has ever been billed to this account"),
			},
		},
	})

	registry.Document("/billing/cancel", api.PathItem{
		Post: &api.Operation{
			Tags:        []string{"billing"},
			Summary:     "Cancel the subscription",
			Description: "Stops at the end of the period already paid for, or straight away with immediately.",
			OperationID: "billing.cancel",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"immediately": {Type: "boolean"},
			})),
			Responses: map[string]api.Response{
				"200": api.JSON("The subscription as it now stands", dataOf(subscriptionSchema())),
				"401": api.Failure("Authentication required"),
				"404": api.Failure("No subscription to change"),
			},
		},
	})

	registry.Document("/billing/resume", api.PathItem{
		Post: &api.Operation{
			Tags:        []string{"billing"},
			Summary:     "Undo a cancellation",
			Description: "Only while the cancellation has not taken effect yet.",
			OperationID: "billing.resume",
			Responses: map[string]api.Response{
				"200": api.JSON("The subscription as it now stands", dataOf(subscriptionSchema())),
				"401": api.Failure("Authentication required"),
				"404": api.Failure("No subscription to change"),
			},
		},
	})

	registry.Document("/billing/webhook", api.PathItem{
		Post: (&api.Operation{
			Tags:    []string{"billing"},
			Summary: "Where Stripe reports",
			Description: "Public, and authenticated by the Stripe-Signature header over the raw body. " +
				"An event is applied once: replays are acknowledged and dropped.",
			OperationID: "billing.webhook",
			Responses: map[string]api.Response{
				"200": api.JSON("Acknowledged", api.Object(map[string]*api.Schema{
					"received": {Type: "boolean"},
				})),
				"400": api.Failure("Invalid signature"),
			},
		}).Public(),
	})
}

// dataOf wraps a schema the way every handler wraps its payload.
func dataOf(schema *api.Schema) *api.Schema {
	return api.Object(map[string]*api.Schema{"data": schema})
}

func subscriptionSchema() *api.Schema {
	return api.Object(map[string]*api.Schema{
		"status":               api.String(),
		"plan":                 api.String(),
		"active":               {Type: "boolean"},
		"cancel_at_period_end": {Type: "boolean"},
		"current_period_end":   {Type: "string", Format: "date-time"},
		"ended_at":             {Type: "string", Format: "date-time"},
	})
}

func redirectSchema() *api.Schema {
	return api.Object(map[string]*api.Schema{"url": {Type: "string", Format: "uri"}})
}
