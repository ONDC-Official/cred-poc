package credential

import (
	"fmt"

	"credential-service/internal/service/client"
)

type VerifierRegistry struct {
	verifiers map[string]Verifier
}

func NewVerifierRegistry(digioClient *client.DigioClient) *VerifierRegistry {
	return &VerifierRegistry{
		verifiers: map[string]Verifier{
			"PAN":   NewPanHandler(digioClient),
			"GST":   NewGstHandler(digioClient),
			"FSSAI": NewFssaiHandler(digioClient),
			"UDYAM": NewUdyamHandler(digioClient),
		},
	}
}

func (r *VerifierRegistry) Resolve(credType string) (Verifier, error) {
	v, ok := r.verifiers[credType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCredType, credType)
	}
	return v, nil
}

var ErrUnsupportedCredType = fmt.Errorf("unsupported credential type")
