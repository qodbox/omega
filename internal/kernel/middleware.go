package kernel

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

const RequestIDKey = "requestid"

func RequestLogger(log zerolog.Logger, pretty bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		elapsed := time.Since(start)

		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				status = fiberErr.Code
			}
		}

		event := log.Info()
		switch {
		case status >= fiber.StatusInternalServerError:
			event = log.Error()
		case status >= fiber.StatusBadRequest:
			event = log.Warn()
		}

		if pretty {
			event.Msg(requestLine(c.Method(), c.OriginalURL(), status, elapsed))
			return err
		}

		event.
			Str("method", c.Method()).
			Str("path", c.OriginalURL()).
			Int("status", status).
			Dur("duration", elapsed).
			Str("ip", c.IP()).
			Str("request_id", requestID(c)).
			Msg("request")

		return err
	}
}

func RequestID() fiber.Handler {
	return requestid.New(requestid.Config{ContextKey: RequestIDKey})
}

func requestID(c *fiber.Ctx) string {
	if id, ok := c.Locals(RequestIDKey).(string); ok {
		return id
	}
	return ""
}
