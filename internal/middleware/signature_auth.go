package middleware

import (
	"errors"

	"credential-service/internal/auth"
	"credential-service/pkg/ondcauth"

	"github.com/gofiber/fiber/v2"
)

const (
	rawBodyLocalKey  = "rawBody"
	identityLocalKey = "ondcIdentity"
)

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
		// Pass the request context so an outbound registry lookup is cancelled with
		// the request rather than outliving it.
		identity, err := verifier.VerifyRequest(c.UserContext(), authHeader, body)
		if err != nil {
			if errors.Is(err, auth.ErrMissingAuthorization) || errors.Is(err, auth.ErrUnauthorized) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "authorization verification failed",
			})
		}

		c.Locals(identityLocalKey, identity)
		return c.Next()
	}
}

// VerifiedIdentity returns the caller SignatureAuth authenticated, false when no
// verifier ran.
func VerifiedIdentity(c *fiber.Ctx) (auth.Identity, bool) {
	identity, ok := c.Locals(identityLocalKey).(auth.Identity)
	return identity, ok
}

// ClaimedIdentity is the identity named in the header without a signature check: a
// claim, never proof. Used when auth is disabled.
func ClaimedIdentity(c *fiber.Ctx) auth.Identity {
	if identity, ok := VerifiedIdentity(c); ok {
		return identity
	}
	subscriberID, uniqueKeyID, err := ondcauth.ParseKeyID(c.Get(fiber.HeaderAuthorization))
	if err != nil {
		return auth.Identity{}
	}
	return auth.Identity{SubscriberID: subscriberID, UniqueKeyID: uniqueKeyID}
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
