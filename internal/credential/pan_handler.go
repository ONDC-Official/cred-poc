package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"credential-service/internal/service/client"
)

const panEndpoint = "/v3/client/kyc/fetch_id_data/PAN"

var panPattern = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)

type PanCredData struct {
	IDNo string `json:"id_no"`
	Name string `json:"name"`
	Dob  string `json:"dob"`
}

type panDigioResponse struct {
	PAN      string `json:"pan"`
	Category string `json:"category"`
	Status   string `json:"status"`
	FullName string `json:"full_name"`
}

func NewPanHandler(digioClient *client.DigioClient) Verifier {
	return NewDigioVerifier(DigioVerifierConfig{
		Client:          digioClient,
		Endpoint:        panEndpoint,
		CredType:        "PAN",
		ValidateFn:      validatePanCredData,
		BuildRequestFn:  buildPanDigioRequest,
		ParseResponseFn: parsePanDigioResponse,
	})
}

func validatePanCredData(data json.RawMessage) error {
	var d PanCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("invalid PAN cred_data: %w", err)
	}
	normalized := strings.ToUpper(strings.TrimSpace(d.IDNo))
	if !panPattern.MatchString(normalized) {
		return fmt.Errorf("%w: invalid PAN format", ErrInvalidCredID)
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("PAN cred_data: name is required")
	}
	if strings.TrimSpace(d.Dob) == "" {
		return fmt.Errorf("PAN cred_data: dob is required")
	}
	return nil
}

func buildPanDigioRequest(data json.RawMessage) (any, error) {
	var d PanCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return map[string]string{
		"id_no": strings.ToUpper(strings.TrimSpace(d.IDNo)),
		"name":  strings.TrimSpace(d.Name),
		"dob":   strings.TrimSpace(d.Dob),
	}, nil
}

func parsePanDigioResponse(body []byte) (*VerificationResult, error) {
	var resp panDigioResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse PAN response: %w", err)
	}

	credData := map[string]any{
		"pan":       resp.PAN,
		"category":  resp.Category,
		"status":    resp.Status,
		"full_name": resp.FullName,
	}

	return &VerificationResult{
		Success:      true,
		CredID:       resp.PAN,
		VerifiedData: credData,
	}, nil
}
