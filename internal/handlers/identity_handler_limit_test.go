package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"credential-service/internal/models"
	"credential-service/internal/service"

	"github.com/google/uuid"
)

type fixedCounter int64

func (c fixedCounter) CountVerifyVendorCalls(context.Context, uuid.UUID) (int64, error) {
	return int64(c), nil
}

func TestVerifyIdentityLimitExceeded(t *testing.T) {
	t.Parallel()

	enumCache := models.NewEnumCache([]models.EnumType{
		{ID: uuid.New(), Category: models.CategoryCredType, Value: models.CredTypePAN},
	})
	limiter := service.NewVerifyLimiter(fixedCounter(5000), enumCache, 5000)

	app := setupTestAppWithLimiter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("provider must not be called once the limit is reached")
	}, limiter)

	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe","dob":"01/01/1990"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["error"] != "limit exceeded for PAN" {
		t.Fatalf("unexpected error message: %v", result)
	}
}
