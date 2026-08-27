package routes

import (
	"github.com/gofiber/fiber/v2"

	"omega"
	controller "omega/app/controllers/currency"
	"omega/internal/api"
	jwt "omega/internal/auth"
)

// registerCurrency publishes the currency catalogue and the setting that picks
// one. Both exist whether or not billing is enabled: reading figures in one's
// own currency is not a billing feature.
func registerCurrency(app *omega.App, group fiber.Router, guard *jwt.Guard, registry *api.Registry) {
	local := controller.NewController(app.DB, app.Currency, guard)

	group.Get("/currencies", local.Catalogue).Name("currencies.index")
	group.Put("/settings/currency", guard.Required(), local.Choose).Name("settings.currency")

	documentCurrency(registry)
}

func documentCurrency(registry *api.Registry) {
	registry.Document("/currencies", api.PathItem{
		Get: (&api.Operation{
			Tags:    []string{"currency"},
			Summary: "The currencies on offer",
			Description: "The currency amounts are stored in, what may be chosen, and the day's rates " +
				"from a central bank. One place queries the provider and serves the same table to " +
				"everyone; a client converts for display and never fetches a rate of its own.",
			OperationID: "currencies.index",
			Responses: map[string]api.Response{
				"200": api.JSON("The catalogue and the rates", dataOf(api.Object(map[string]*api.Schema{
					"base": api.String(),
					"currencies": {Type: "array", Items: api.Object(map[string]*api.Schema{
						"code":     api.String(),
						"symbol":   api.String(),
						"name":     api.String(),
						"decimals": api.Integer(),
					})},
					"rates": api.Dictionary(&api.Schema{Type: "number"}),
				}))),
			},
		}).Public(),
	})

	registry.Document("/settings/currency", api.PathItem{
		Put: &api.Operation{
			Tags:    []string{"currency"},
			Summary: "Choose the currency to read in",
			Description: "A display preference: it changes what the caller is shown, never what is " +
				"stored, and never what a card is charged. A currency that is not on offer is refused " +
				"rather than stored, since it would convert nothing.",
			OperationID: "settings.currency",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"currency": {Type: "string", MinLength: 3, MaxLength: 3},
			}, "currency")),
			Responses: map[string]api.Response{
				"200": api.JSON("The preference as it now stands", dataOf(api.Object(map[string]*api.Schema{
					"currency": api.String(),
					"base":     api.String(),
					"rate":     {Type: "number"},
				}))),
				"401": api.Failure("Authentication required"),
				"422": api.Failure("This currency is not one of those on offer"),
			},
		},
	})
}
