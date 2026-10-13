package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"credential-service/internal/service"
)

// subscriberCounter returns a fixed count per subscriber_id; unknown ids have used none.
type subscriberCounter map[string]int64

func (c subscriberCounter) CountVerifyVendorCallsBySubscriber(_ context.Context, subscriberID string) (int64, error) {
	return c[subscriberID], nil
}

const panBody = `{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe","dob":"01/01/1990"}`

func verifyRequestAs(subscriberID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewBufferString(panBody))
	req.Header.Set("Content-Type", "application/json")
	if subscriberID != "" {
		// Auth is off in these tests, so only the keyId is read; the signature is not checked.
		req.Header.Set("Authorization",
			`Signature keyId="`+subscriberID+`|key-1|ed25519",algorithm="ed25519",created="1",expires="2",headers="(created) (expires) digest",signature="x"`)
	}
	return req
}

func TestVerifyIdentityLimitExceeded(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(subscriberCounter{"np-a.example.com": 5000}, 5000)
	app := setupTestAppWithLimiter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("provider must not be called once the limit is reached")
	}, limiter)

	resp, err := app.Test(verifyRequestAs("np-a.example.com"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if want := "Verification limit reached. Please contact the ONDC team."; result["error"] != want {
		t.Fatalf("unexpected error message: %v", result)
	}
}

func TestVerifyIdentityLimitIsPerSubscriber(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	limiter := service.NewVerifyLimiter(subscriberCounter{"np-a.example.com": 5000}, 5000)
	app := setupTestAppWithLimiter(t, func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}, limiter)

	resp, err := app.Test(verifyRequestAs("np-b.example.com"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("subscriber B must not be limited by A's usage, got %d", resp.StatusCode)
	}
	if providerCalls.Load() == 0 {
		t.Fatal("expected subscriber B's call to reach the provider")
	}
}

func TestVerifyIdentityLimitNeedsSubscriber(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(subscriberCounter{}, 5000)
	app := setupTestAppWithLimiter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("provider must not be called without a subscriber_id")
	}, limiter)

	resp, err := app.Test(verifyRequestAs(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
