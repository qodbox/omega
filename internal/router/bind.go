package router

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type modelKey[T any] struct{}

type BindOptions struct {
	Param string

	Column string

	Preload []string

	Scope func(c *fiber.Ctx, tx *gorm.DB) *gorm.DB
}

func Model[T any](db *gorm.DB, opts ...BindOptions) fiber.Handler {
	opt := BindOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Param == "" {
		opt.Param = "id"
	}
	if opt.Column == "" {
		opt.Column = "id"
	}

	return func(c *fiber.Ctx) error {
		value := c.Params(opt.Param)
		if value == "" {
			return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("missing route parameter %q", opt.Param))
		}

		tx := db.WithContext(c.UserContext())
		for _, relation := range opt.Preload {
			tx = tx.Preload(relation)
		}
		if opt.Scope != nil {
			tx = opt.Scope(c, tx)
		}

		var model T
		err := tx.Where(fmt.Sprintf("%s = ?", opt.Column), value).First(&model).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return fiber.ErrNotFound
		case err != nil:
			return fmt.Errorf("bind %T: %w", model, err)
		}

		c.Locals(modelKey[T]{}, &model)
		return c.Next()
	}
}

func Bound[T any](c *fiber.Ctx) (*T, bool) {
	model, ok := c.Locals(modelKey[T]{}).(*T)
	return model, ok
}

func MustBound[T any](c *fiber.Ctx) *T {
	model, ok := Bound[T](c)
	if !ok {
		var zero T
		panic(fmt.Sprintf("router: no %T bound on this route — is router.Model[%T] in the chain?", zero, zero))
	}
	return model
}
