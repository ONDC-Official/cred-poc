package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"credential-service/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

// GetResults' missing/invalid-request_id paths return before ever touching
// the service, so they're testable with a nil CredentialService — the
// 200/404 DB-backed cases need a real database and are deferred to Phase 0
// (docs/IMPLEMENTATION_ROADMAP.md), same constraint as credential_service_test.go.

func newGetResultsTestApp() *fiber.App {
	h := handlers.NewCredentialHandler(nil)
	app := fiber.New()
	app.Get("/credential", h.GetResults)
	return app
}

func TestGetResultsInvalidRequestID(t *testing.T) {
	t.Parallel()

	app := newGetResultsTestApp()

	req := httptest.NewRequest(http.MethodGet, "/credential?request_id=not-a-uuid", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetResultsMissingRequestID(t *testing.T) {
	t.Parallel()

	app := newGetResultsTestApp()

	req := httptest.NewRequest(http.MethodGet, "/credential", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
