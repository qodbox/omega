package policies

import (
	"context"

	"omega/app/models"
	"omega/internal/policy"
)

var extra []func(*policy.Registry)

func Extend(register func(*policy.Registry)) { extra = append(extra, register) }

func Register(gate *policy.Registry) {
	defer func() {
		for _, register := range extra {
			register(gate)
		}
	}()

	gate.Before(func(ctx context.Context, actor any, ability string) (bool, bool) {
		user, ok := actor.(*models.User)
		if ok && user.Is(models.RoleAdmin) {
			return true, true
		}
		return false, false
	})

	// The users resource is declared with api.Owner("id"): these grants reach
	// the member's own account only. "users.any" lifts that scope; admins hold
	// it through the wildcard. Never grant "users.*" to members: a resource
	// wildcard includes "users.any".
	gate.Grant(models.RoleUser,
		"users.list",
		"users.view",
		"profile.update",
	)

	gate.Grant(models.RoleAdmin, "*")

	// On the generated routes the subject is the loaded *models.User.
	gate.Define("users.update", func(ctx context.Context, actor, subject any) bool {
		user, ok := actor.(*models.User)
		if !ok {
			return false
		}
		if user.Is(models.RoleAdmin) {
			return true
		}

		target, ok := subject.(*models.User)
		return ok && target.ID == user.ID
	})

	gate.Define("users.delete", func(ctx context.Context, actor, subject any) bool {
		user, ok := actor.(*models.User)
		if !ok || !user.Is(models.RoleAdmin) {
			return false
		}

		target, ok := subject.(*models.User)
		return !ok || target.ID != user.ID
	})

	gate.Define("users.create", func(ctx context.Context, actor, subject any) bool {
		user, ok := actor.(*models.User)
		return ok && user.Is(models.RoleAdmin)
	})
}
