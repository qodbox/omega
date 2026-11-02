package unit

import (
	"golang.org/x/crypto/bcrypt"

	"omega/app/models"
)

func init() {
	// bcrypt at the production cost takes seconds under the race detector;
	// the suite tests the logic, not the work factor.
	models.PasswordCost = bcrypt.MinCost + 1
}
