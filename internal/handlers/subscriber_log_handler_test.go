package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"credential-service/internal/handlers"
	"credential-service/internal/repository"

	"github.com/gofiber/fiber/v2"
)

// Every case below is rejected before the handler reaches the database, so a repository
// over a nil *gorm.DB is enough. The 200 paths need a real Postgres.
func newSubscriberLogApp(repo *repository.CredentialRequestRepository) *fiber.App {
	h := handlers.NewSubscriberLogHandler(repo, nil)
	app := fiber.New()
	app.Get("/subscriber/:id", h.List)
	return app
}

func getSubscriberLogs(t *testing.T, app *fiber.App, target string) int {
	t.Helper()

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, target, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

func TestSubscriberLogsUnavailableWithoutDatabase(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(nil)

	if status := getSubscriberLogs(t, app, "/subscriber/np.example.com"); status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no database, got %d", status)
	}
}

func TestSubscriberLogsRejectsBlankSubscriberID(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(repository.NewCredentialRequestRepository(nil))

	// %20 survives routing but trims to empty.
	if status := getSubscriberLogs(t, app, "/subscriber/%20"); status != http.StatusBadRequest {
		t.Fatalf("expected 400 for a blank subscriber id, got %d", status)
	}
}

func TestSubscriberLogsRejectsBadTimestamps(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(repository.NewCredentialRequestRepository(nil))

	for _, target := range []string{
		"/subscriber/np.example.com?from=yesterday",
		"/subscriber/np.example.com?to=2026-13-45",
		"/subscriber/np.example.com?from=2026-09-16T00:00:00Z&to=2026-09-15T00:00:00Z",
	} {
		if status := getSubscriberLogs(t, app, target); status != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", target, status)
		}
	}
}

func TestSubscriberLogsRejectsWindowWiderThan31Days(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(repository.NewCredentialRequestRepository(nil))

	target := "/subscriber/np.example.com?from=2026-01-01T00:00:00Z&to=2026-09-16T00:00:00Z"
	if status := getSubscriberLogs(t, app, target); status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an over-wide window, got %d", status)
	}
}

func TestSubscriberLogsRejectsBadLimit(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(repository.NewCredentialRequestRepository(nil))

	for _, target := range []string{
		"/subscriber/np.example.com?limit=0",
		"/subscriber/np.example.com?limit=201",
		"/subscriber/np.example.com?limit=abc",
		"/subscriber/np.example.com?limit=-5",
	} {
		if status := getSubscriberLogs(t, app, target); status != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", target, status)
		}
	}
}

func TestSubscriberLogsRejectsBadPage(t *testing.T) {
	t.Parallel()

	app := newSubscriberLogApp(repository.NewCredentialRequestRepository(nil))

	for _, target := range []string{
		"/subscriber/np.example.com?page=0",
		"/subscriber/np.example.com?page=-1",
		"/subscriber/np.example.com?page=abc",
	} {
		if status := getSubscriberLogs(t, app, target); status != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", target, status)
		}
	}
}
