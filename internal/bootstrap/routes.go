package bootstrap

import (
	"credential-service/internal/auth"
	appmiddleware "credential-service/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func registerRoutes(app *fiber.App, h *Handlers, verifier *auth.Verifier) {
	RegisterRoutes(app, h, verifier)
}

func RegisterRoutes(app *fiber.App, h *Handlers, verifier *auth.Verifier) {
	app.Get("/health", h.Health.Check)

	protected := app.Group("", appmiddleware.CaptureRawBody(), appmiddleware.SignatureAuth(verifier))
	protected.Post("/verify-identity", h.Identity.VerifyIdentity)

	if h.Credential != nil {
		// Registry calls these on behalf of an onboarded participant_id.
		// Path per Credential Service Implementation Plan.md's Credential API
		// section ("Path: /credential"). GetResults has no doc precedent (it's
		// a net-new read endpoint) but follows the same base path, keyed by a
		// ?request_id= query param rather than a path segment — a bare path
		// segment here would be ambiguous between "the batch's request_id"
		// and "one credential's own id".
		protected.Post("/credential", h.Credential.SubmitCredentials)
		protected.Get("/credential", h.Credential.GetResults)
	}
}
