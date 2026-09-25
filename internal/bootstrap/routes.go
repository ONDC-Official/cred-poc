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
	app.Get("/", h.Health.Check)

	// Deliberately outside the protected group: you need this to produce the header the
	// group requires. It signs anything with this service's own key, so it is registered
	// only when enabled (development by default) and must never be publicly reachable.
	if h.Auth != nil {
		app.Post("/generate-header", h.Auth.GenerateHeader)
	}

	// Disabled for now: not to be exposed. Outside the protected group when enabled:
	// authenticated by a static token, not a signature.
	// if h.SubscriberLogs != nil {
	// 	app.Get("/subscriber/:id", appmiddleware.AccessToken(h.accessToken), h.SubscriberLogs.List)
	// }

	// Auth is attached per route, not via app.Group("", ...): an empty-prefix group
	// runs its middleware on every path, so unknown or disabled routes would answer
	// 401 instead of 404 and look like they exist.
	captureRawBody := appmiddleware.CaptureRawBody()
	signatureAuth := appmiddleware.SignatureAuth(verifier)
	protected := func(handler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{captureRawBody, signatureAuth, handler}
	}

	app.Post("/verify", protected(h.Identity.VerifyIdentity)...)

	if h.Credential != nil {
		// Registry calls these on behalf of an onboarded participant_id.
		// Path per Credential Service Implementation Plan.md's Credential API
		// section ("Path: /credential"). GetResults has no doc precedent (it's
		// a net-new read endpoint) but follows the same base path, keyed by a
		// ?request_id= query param rather than a path segment — a bare path
		// segment here would be ambiguous between "the batch's request_id"
		// and "one credential's own id".
		app.Post("/credential", protected(h.Credential.SubmitCredentials)...)
		app.Get("/credential", protected(h.Credential.GetResults)...)
	}
}
