package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"credential-service/internal/service/client"
)

func TestClientPostSuccess(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	var receivedBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})

	respBody, statusCode, err := digioClient.Post(context.Background(), "/test", map[string]string{"id_no": "ABCDE1234F"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}

	if receivedAuth != "Basic test-token" {
		t.Fatalf("expected Basic auth header, got %q", receivedAuth)
	}

	if receivedBody["id_no"] != "ABCDE1234F" {
		t.Fatalf("expected id_no in payload, got %v", receivedBody)
	}

	if !strings.Contains(string(respBody), "ok") {
		t.Fatalf("unexpected response body: %s", string(respBody))
	}
}

func TestClientPostHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream failure"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})

	_, statusCode, err := digioClient.Post(context.Background(), "/test", map[string]string{"id_no": "ABCDE1234F"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if statusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", statusCode)
	}
}

func TestClientPostInvalidJSONResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})

	respBody, statusCode, err := digioClient.Post(context.Background(), "/test", map[string]string{"id_no": "ABCDE1234F"})
	if err != nil {
		t.Fatalf("client should return body even for invalid JSON: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}

	if string(respBody) != "not-json" {
		t.Fatalf("unexpected body: %s", string(respBody))
	}
}

func TestClientPostTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 50 * time.Millisecond,
	})

	_, _, err := digioClient.Post(context.Background(), "/test", map[string]string{"id_no": "ABCDE1234F"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
