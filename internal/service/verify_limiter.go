package service

import (
	"context"
	"errors"
	"fmt"
)

// ErrVerifyLimitExceeded is returned once a subscriber has used up its /verify limit.
// Its text is returned to the caller verbatim, hence the sentence form.
var ErrVerifyLimitExceeded = errors.New("Verification limit reached. Please contact the ONDC team.")

// ErrSubscriberUnknown is returned when a limit is set but the request names no
// subscriber_id to count against. Only reachable with signature auth disabled.
var ErrSubscriberUnknown = errors.New("subscriber_id missing from authorization header")

// VendorCallCounter counts one subscriber's /verify calls that reached a provider.
// Implemented by repository.CredentialRequestRepository.
type VendorCallCounter interface {
	CountVerifyVendorCallsBySubscriber(ctx context.Context, subscriberID string) (int64, error)
}

// VerifyLimiter enforces one lifetime cap on /verify per subscriber_id, across all
// cred_types, counted from the rows VerifyIdentityLogger writes. The row lands after
// the response, so concurrent requests at the boundary can overshoot the cap by roughly
// the number in flight.
type VerifyLimiter struct {
	counter VendorCallCounter
	limit   int
}

func NewVerifyLimiter(counter VendorCallCounter, limit int) *VerifyLimiter {
	return &VerifyLimiter{counter: counter, limit: limit}
}

// Check returns ErrVerifyLimitExceeded when subscriberID has reached the limit. A nil
// limiter or a limit of 0 allows everything. An empty subscriberID is refused with
// ErrSubscriberUnknown so the cap cannot be skipped. A count failure is returned, so
// an outage cannot silently bypass the cap.
func (l *VerifyLimiter) Check(ctx context.Context, subscriberID string) error {
	if l == nil || l.counter == nil || l.limit <= 0 {
		return nil
	}
	if subscriberID == "" {
		return ErrSubscriberUnknown
	}

	used, err := l.counter.CountVerifyVendorCallsBySubscriber(ctx, subscriberID)
	if err != nil {
		return fmt.Errorf("count verify calls for subscriber %s: %w", subscriberID, err)
	}
	if used >= int64(l.limit) {
		return ErrVerifyLimitExceeded
	}
	return nil
}
