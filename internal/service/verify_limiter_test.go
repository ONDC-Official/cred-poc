package service_test

import (
	"context"
	"errors"
	"testing"

	"credential-service/internal/models"
	"credential-service/internal/service"

	"github.com/google/uuid"
)

type fakeCounter struct {
	count int64
	err   error
	calls int
}

func (f *fakeCounter) CountVerifyVendorCalls(context.Context, uuid.UUID) (int64, error) {
	f.calls++
	return f.count, f.err
}

func panEnumCache() *models.EnumCache {
	return models.NewEnumCache([]models.EnumType{
		{ID: uuid.New(), Category: models.CategoryCredType, Value: models.CredTypePAN},
	})
}

func TestVerifyLimiterNilAllows(t *testing.T) {
	t.Parallel()

	var limiter *service.VerifyLimiter
	if err := limiter.Check(context.Background(), "PAN"); err != nil {
		t.Fatalf("expected a nil limiter to allow, got %v", err)
	}
}

func TestVerifyLimiterZeroLimitAllows(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{count: 100}
	limiter := service.NewVerifyLimiter(counter, panEnumCache(), 0)

	if err := limiter.Check(context.Background(), "PAN"); err != nil {
		t.Fatalf("expected a zero limit to be unlimited, got %v", err)
	}
	if counter.calls != 0 {
		t.Fatalf("expected no count with a zero limit, got %d calls", counter.calls)
	}
}

func TestVerifyLimiterUnknownEnumAllows(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{count: 100}
	limiter := service.NewVerifyLimiter(counter, models.NewEnumCache(nil), 1)

	if err := limiter.Check(context.Background(), "PAN"); err != nil {
		t.Fatalf("expected a type with no enum row to be allowed, got %v", err)
	}
}

func TestVerifyLimiterBelowLimitAllows(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(&fakeCounter{count: 4999}, panEnumCache(), 5000)

	if err := limiter.Check(context.Background(), "PAN"); err != nil {
		t.Fatalf("expected below-limit call to be allowed, got %v", err)
	}
}

func TestVerifyLimiterAtLimitRefuses(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(&fakeCounter{count: 5000}, panEnumCache(), 5000)

	err := limiter.Check(context.Background(), "PAN")
	if !errors.Is(err, service.ErrVerifyLimitExceeded) {
		t.Fatalf("expected ErrVerifyLimitExceeded, got %v", err)
	}
	if err.Error() != "limit exceeded for PAN" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestVerifyLimiterCountErrorFailsClosed(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(&fakeCounter{err: errors.New("db down")}, panEnumCache(), 5000)

	err := limiter.Check(context.Background(), "PAN")
	if err == nil || errors.Is(err, service.ErrVerifyLimitExceeded) {
		t.Fatalf("expected a count error, got %v", err)
	}
}
