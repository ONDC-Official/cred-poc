package service

import (
	"context"
	"errors"
	"fmt"

	"credential-service/internal/models"

	"github.com/google/uuid"
)

// ErrVerifyLimitExceeded is returned once a cred_type has used up its /verify limit.
var ErrVerifyLimitExceeded = errors.New("limit exceeded")

// VendorCallCounter counts /verify calls of one cred_type that reached a provider.
// Implemented by repository.CredentialRequestRepository.
type VendorCallCounter interface {
	CountVerifyVendorCalls(ctx context.Context, credTypeID uuid.UUID) (int64, error)
}

// VerifyLimiter enforces one lifetime cap on /verify, applied to each cred_type
// separately and counted from the rows VerifyIdentityLogger writes. The row lands after
// the response, so concurrent requests at the boundary can overshoot the cap by roughly
// the number in flight.
type VerifyLimiter struct {
	counter   VendorCallCounter
	enumCache *models.EnumCache
	limit     int
}

func NewVerifyLimiter(counter VendorCallCounter, enumCache *models.EnumCache, limit int) *VerifyLimiter {
	return &VerifyLimiter{counter: counter, enumCache: enumCache, limit: limit}
}

// Check returns ErrVerifyLimitExceeded when credType has reached the limit. A nil
// limiter, a limit of 0, or a type with no enum row is allowed. A count failure is
// returned, so an outage cannot silently bypass the cap.
func (l *VerifyLimiter) Check(ctx context.Context, credType string) error {
	if l == nil || l.counter == nil || l.enumCache == nil || l.limit <= 0 {
		return nil
	}

	credTypeID := l.enumCache.CredTypeID(credType)
	if credTypeID == uuid.Nil {
		return nil
	}

	used, err := l.counter.CountVerifyVendorCalls(ctx, credTypeID)
	if err != nil {
		return fmt.Errorf("count %s verify calls: %w", credType, err)
	}
	if used >= int64(l.limit) {
		return fmt.Errorf("%w for %s", ErrVerifyLimitExceeded, credType)
	}
	return nil
}
