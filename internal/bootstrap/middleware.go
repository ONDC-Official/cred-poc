package bootstrap

import (
	appmiddleware "credential-service/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func registerMiddleware(app *fiber.App) {
	app.Use(recover.New())
	app.Use(appmiddleware.Logger())
}
