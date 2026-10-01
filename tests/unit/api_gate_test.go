package unit

import (
	"context"

	"omega/internal/api"
)

// Un registre sans gate refuse tout : c'est le defaut voulu en production, parce
// qu'un schema GraphQL monte sans policies servait auparavant chaque table a
// n'importe quel appelant authentifie. Les cas qui couvrent la mecanique des
// resolvers, et non l'autorisation, passent par ici pour dire explicitement
// qu'ils travaillent gate ouvert.
func openGate(registry *api.Registry) *api.Registry {
	registry.Protect(
		func(context.Context, any, string, ...any) bool { return true },
		func(context.Context) any { return unitActor{} },
	)
	return registry
}

type unitActor struct{}

func (unitActor) Role() string { return "admin" }
