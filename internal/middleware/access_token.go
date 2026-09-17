package middleware

import (
	"crypto/subtle"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// AccessToken guards a route with a static shared key, sent as a bare
// `Authorization: <token>` header. Only the subscriber log route uses it.
func AccessToken(token string) fiber.Handler {
	expected := strings.TrimSpace(token)

	return func(c *fiber.Ctx) error {
		provided := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))

		// Constant time: a timed comparison would leak the token character by character.
		if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			slog.WarnContext(c.UserContext(), "access token rejected", "method", c.Method(), "path", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid or missing access token",
			})
		}

		return c.Next()
	}
}
