package kernel

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"omega/internal/validation"
)

func ErrorHandler(log zerolog.Logger, debug, pretty bool) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		status := fiber.StatusInternalServerError
		message := "An internal error occurred."

		var invalid *validation.Error
		if errors.As(err, &invalid) {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
				"error":      "Validation failed.",
				"status":     fiber.StatusUnprocessableEntity,
				"errors":     invalid.Fields,
				"request_id": requestID(c),
			})
		}

		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			status = fiberErr.Code
			message = fiberErr.Message
		}

		if !pretty || status >= fiber.StatusInternalServerError {
			event := log.Warn()
			if status >= fiber.StatusInternalServerError {
				event = log.Error()
			}
			event.Err(err).
				Int("status", status).
				Str("method", c.Method()).
				Str("path", c.Path()).
				Str("request_id", requestID(c)).
				Msg("request failed")
		}

		if status >= fiber.StatusInternalServerError && debug {
			message = err.Error()
		}

		return c.Status(status).JSON(fiber.Map{
			"error":      message,
			"status":     status,
			"request_id": requestID(c),
		})
	}
}
