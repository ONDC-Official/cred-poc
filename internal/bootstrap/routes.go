package bootstrap

import "github.com/gofiber/fiber/v2"

func registerRoutes(app *fiber.App, h *Handlers) {
	RegisterRoutes(app, h)
}

func RegisterRoutes(app *fiber.App, h *Handlers) {
	app.Get("/health", h.Health.Check)
	app.Post("/verify-identity", h.Identity.VerifyIdentity)

	if h.Credential != nil {
		// Path per Credential Service Implementation Plan.md's Credential API
		// section ("Path: /credential"). GetResults has no doc precedent (it's
		// a net-new read endpoint) but follows the same base path, keyed by a
		// ?request_id= query param rather than a path segment — a bare path
		// segment here would be ambiguous between "the batch's request_id"
		// and "one credential's own id".
		app.Post("/credential", h.Credential.SubmitCredentials)
		app.Get("/credential", h.Credential.GetResults)
	}
}
