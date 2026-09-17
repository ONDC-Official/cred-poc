package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"credential-service/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

const testToken = "s3cr3t-access-token"

func newAccessTokenApp(token string) *fiber.App {
	app := fiber.New()
	app.Get("/subscriber/:id", middleware.AccessToken(token), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func requestWithAuthorization(t *testing.T, app *fiber.App, header string) int {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/subscriber/np.example.com", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

func TestAccessTokenAllowsExactToken(t *testing.T) {
	t.Parallel()

	if status := requestWithAuthorization(t, newAccessTokenApp(testToken), testToken); status != http.StatusOK {
		t.Fatalf("expected 200 for the configured token, got %d", status)
	}
}

func TestAccessTokenIgnoresSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	if status := requestWithAuthorization(t, newAccessTokenApp(testToken), "  "+testToken+"  "); status != http.StatusOK {
		t.Fatalf("expected 200 for a padded token, got %d", status)
	}
}

func TestAccessTokenRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	if status := requestWithAuthorization(t, newAccessTokenApp(testToken), ""); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 without an Authorization header, got %d", status)
	}
}

func TestAccessTokenRejectsWrongToken(t *testing.T) {
	t.Parallel()

	if status := requestWithAuthorization(t, newAccessTokenApp(testToken), "not-the-token"); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong token, got %d", status)
	}
}

func TestAccessTokenRejectsBearerForm(t *testing.T) {
	t.Parallel()

	// The header value is the token itself, so a Bearer prefix must not be accepted.
	if status := requestWithAuthorization(t, newAccessTokenApp(testToken), "Bearer "+testToken); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a Bearer-prefixed token, got %d", status)
	}
}

func TestAccessTokenRejectsEverythingWhenUnconfigured(t *testing.T) {
	t.Parallel()

	// routes.go does not register the route without a token, but an empty token must
	// not mean "let everyone in" either.
	app := newAccessTokenApp("")

	if status := requestWithAuthorization(t, app, ""); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no token configured, got %d", status)
	}
	if status := requestWithAuthorization(t, app, ""); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an empty header with no token configured, got %d", status)
	}
}
