package service

import (
	"context"
	"encoding/json"

	"credential-service/internal/credential"
)

// IdentityService is the single function both /verify and the
// async /credential flow call through to verify a credential against
// the configured provider — CredentialService.ProcessCredentialRequest
// calls VerifyIdentity directly rather than resolving its own handler
// and calling Process, so there is exactly one code path that talks to
// a KYC provider for a single credential, not two that happen to share
// a helper.
type IdentityService struct {
	registry *credential.VerifierRegistry
}

func NewIdentityService(registry *credential.VerifierRegistry) *IdentityService {
	return &IdentityService{registry: registry}
}

// VerifyIdentity resolves the verifier for credType and runs it through the
// shared ConfigVerifier pipeline. credData is the same {"id_no","name","dob"}
// shape used everywhere else (credential_service.go's buildCredData, and
// the persisted credential_requests.cred_data column) — callers with flat
// fields (identity_handler.go) marshal into this shape themselves; callers
// that already have it (CredentialService) pass it straight through.
//
// Returns the full VerificationResult, not just the provider's raw response —
// interpreting it (HTTP status mapping, raw-body extraction, retry
// decisions) is each caller's own concern, not this function's.
func (s *IdentityService) VerifyIdentity(ctx context.Context, credType string, credData json.RawMessage) (*credential.VerificationResult, error) {
	verifier, err := s.registry.Resolve(credType)
	if err != nil {
		return nil, err
	}
	return verifier.Process(ctx, credData)
}
