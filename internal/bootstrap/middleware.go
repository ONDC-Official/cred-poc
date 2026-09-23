package bootstrap

import (
	appmiddleware "credential-service/internal/middleware"

	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func registerMiddleware(app *fiber.App) {
	app.Use(recover.New())
	// Server span per request, continuing an inbound traceparent, stored in c.UserContext().
	// Handlers must pass c.UserContext() (not c.Context()) downstream or the trace breaks.
	app.Use(otelfiber.Middleware(otelfiber.WithNext(isHealthCheck)))
	app.Use(appmiddleware.Logger(nil))
}

// isHealthCheck skips /health and /, which the ALB and compose healthcheck hit.
func isHealthCheck(c *fiber.Ctx) bool {
	return c.Path() == "/health" || c.Path() == "/"
}
