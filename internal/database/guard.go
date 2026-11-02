package database

import (
	"errors"
	"fmt"
	"strings"
)

// ErrProduction is returned by GuardDestructive in production without force.
var ErrProduction = errors.New("refusing to run in production")

// GuardDestructive refuses a command that destroys or rewrites data (a
// rollback, a reset, a fresh database, the seeders) when the environment is
// production, unless force is set.
func GuardDestructive(env, command string, force bool) error {
	if force || !strings.EqualFold(strings.TrimSpace(env), "production") {
		return nil
	}
	return fmt.Errorf("%w: %s changes or deletes data; pass --force to run it anyway", ErrProduction, command)
}
