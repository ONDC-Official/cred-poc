package middleware

import (
	"errors"

	"credential-service/internal/auth"

	"github.com/gofiber/fiber/v2"
)

const rawBodyLocalKey = "rawBody"

// SignatureAuth rejects requests that are not signed by the registry service.
// NPs do not call credential-service; only the registry service is trusted.
func SignatureAuth(verifier *auth.Verifier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if verifier == nil {
			return c.Next()
		}

		rawBody := c.Locals(rawBodyLocalKey)
		body, _ := rawBody.(string)

		authHeader := c.Get(fiber.HeaderAuthorization)
		if err := verifier.Verify(authHeader, body); err != nil {
			if errors.Is(err, auth.ErrMissingAuthorization) || errors.Is(err, auth.ErrUnauthorized) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "authorization verification failed",
			})
		}

		return c.Next()
	}
}

// CaptureRawBody preserves the exact request body bytes so signature verification
// uses the same string the registry signed.
func CaptureRawBody() fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		c.Locals(rawBodyLocalKey, body)
		return c.Next()
	}
}
