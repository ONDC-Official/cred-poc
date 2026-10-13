package service_test

import (
	"context"
	"errors"
	"testing"

	"credential-service/internal/service"
)

type fakeCounter struct {
	count        int64
	err          error
	calls        int
	subscriberID string
}

func (f *fakeCounter) CountVerifyVendorCallsBySubscriber(_ context.Context, subscriberID string) (int64, error) {
	f.calls++
	f.subscriberID = subscriberID
	return f.count, f.err
}

func TestVerifyLimiterNilAllows(t *testing.T) {
	t.Parallel()

	var limiter *service.VerifyLimiter
	if err := limiter.Check(context.Background(), "np.example.com"); err != nil {
		t.Fatalf("expected a nil limiter to allow, got %v", err)
	}
}

func TestVerifyLimiterZeroLimitAllows(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{count: 100}
	limiter := service.NewVerifyLimiter(counter, 0)

	if err := limiter.Check(context.Background(), ""); err != nil {
		t.Fatalf("expected a zero limit to be unlimited, got %v", err)
	}
	if counter.calls != 0 {
		t.Fatalf("expected no count with a zero limit, got %d calls", counter.calls)
	}
}

func TestVerifyLimiterEmptySubscriberRefused(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{}
	limiter := service.NewVerifyLimiter(counter, 5000)

	if err := limiter.Check(context.Background(), ""); !errors.Is(err, service.ErrSubscriberUnknown) {
		t.Fatalf("expected ErrSubscriberUnknown, got %v", err)
	}
	if counter.calls != 0 {
		t.Fatalf("expected no count for an empty subscriber, got %d calls", counter.calls)
	}
}

func TestVerifyLimiterBelowLimitAllows(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{count: 4999}
	limiter := service.NewVerifyLimiter(counter, 5000)

	if err := limiter.Check(context.Background(), "np.example.com"); err != nil {
		t.Fatalf("expected below-limit call to be allowed, got %v", err)
	}
	if counter.subscriberID != "np.example.com" {
		t.Fatalf("expected count for np.example.com, got %q", counter.subscriberID)
	}
}

func TestVerifyLimiterAtLimitRefuses(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(&fakeCounter{count: 5000}, 5000)

	err := limiter.Check(context.Background(), "np.example.com")
	if !errors.Is(err, service.ErrVerifyLimitExceeded) {
		t.Fatalf("expected ErrVerifyLimitExceeded, got %v", err)
	}
	if want := "Verification limit reached. Please contact the ONDC team."; err.Error() != want {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestVerifyLimiterCountErrorFailsClosed(t *testing.T) {
	t.Parallel()

	limiter := service.NewVerifyLimiter(&fakeCounter{err: errors.New("db down")}, 5000)

	err := limiter.Check(context.Background(), "np.example.com")
	if err == nil || errors.Is(err, service.ErrVerifyLimitExceeded) {
		t.Fatalf("expected a count error, got %v", err)
	}
}
