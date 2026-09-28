package middleware

import (
	"log/slog"
	"time"

	"credential-service/internal/telemetry"

	"github.com/gofiber/fiber/v2"
)

// Logger writes one structured access-log line per request through slog, with the
// request's context so the line carries its trace_id, span_id, and subscriber_id.
// Requests for which skip returns true are not logged. Headers and bodies are never logged.
func Logger(skip func(*fiber.Ctx) bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if skip != nil && skip(c) {
			return c.Next()
		}

		start := time.Now()
		if err := c.Next(); err != nil {
			// Same as Fiber's own logger: run the error handler now so the logged status is
			// the one the client receives.
			if handlerErr := c.App().ErrorHandler(c, err); handlerErr != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}

		status := c.Response().StatusCode()
		level := slog.LevelInfo
		if status >= fiber.StatusInternalServerError {
			level = slog.LevelError
		}

		subscriberID := ClaimedIdentity(c).SubscriberID
		ctx := c.UserContext()
		if subscriberID != "" && telemetry.SubscriberIDFromContext(ctx) == "" {
			ctx = telemetry.WithSubscriberID(ctx, subscriberID)
		}

		slog.Log(ctx, level, "http request",
			"method", c.Method(),
			"path", c.Path(),
			"route", c.Route().Path,
			"status", status,
			"subscriber_id", subscriberID,
			"latency_ms", float64(time.Since(start).Microseconds())/1000,
			"ip", c.IP(),
		)
		return nil
	}
}
