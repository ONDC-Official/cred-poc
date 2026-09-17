package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"credential-service/internal/models"
	"credential-service/internal/repository"
	"credential-service/internal/service"
)

func TestVerifyIdentityLoggerNilIsNoOp(t *testing.T) {
	t.Parallel()

	// The handler holds a nil logger when no database is configured.
	var logger *service.VerifyIdentityLogger

	if err := logger.Record(context.Background(), service.VerifyIdentityLogEntry{CredType: "PAN"}); err != nil {
		t.Fatalf("expected a nil logger to be a no-op, got %v", err)
	}
}

func TestVerifyIdentityLoggerWithoutRepositoryIsNoOp(t *testing.T) {
	t.Parallel()

	logger := service.NewVerifyIdentityLogger(nil, models.NewEnumCache(nil), nil)

	if err := logger.Record(context.Background(), service.VerifyIdentityLogEntry{CredType: "PAN"}); err != nil {
		t.Fatalf("expected a repository-less logger to be a no-op, got %v", err)
	}
}

func TestVerifyIdentityLoggerRejectsUnknownCredType(t *testing.T) {
	t.Parallel()

	// Must fail before any database call: the repository here wraps a nil handle, so
	// reaching it would panic.
	logger := service.NewVerifyIdentityLogger(
		repository.NewCredentialRequestRepository(nil),
		models.NewEnumCache(nil),
		nil,
	)

	err := logger.Record(context.Background(), service.VerifyIdentityLogEntry{
		CredType: "PAN_TO_GST",
		CredData: json.RawMessage(`{"id_no":"ABCDE1234F"}`),
	})
	if err == nil {
		t.Fatal("expected an error for a credential type with no enum row")
	}
	if !strings.Contains(err.Error(), "CRED_TYPE") {
		t.Fatalf("expected the error to name the missing enum, got %v", err)
	}
}
