package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"credential-service/internal/service/client"
)

const gstEndpoint = "/v3/client/kyc/fetch_id_data/GST"

var gstPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

type GstCredData struct {
	IDNo string `json:"id_no"`
}

type gstDigioResponse struct {
	ErrorMessage  string         `json:"error_message"`
	CorporateName string         `json:"corporate_name"`
	GSTIN         string         `json:"gstin"`
	Details       map[string]any `json:"details"`
}

func NewGstHandler(digioClient *client.DigioClient) Verifier {
	return NewDigioVerifier(DigioVerifierConfig{
		Client:          digioClient,
		Endpoint:        gstEndpoint,
		CredType:        "GST",
		ValidateFn:      validateGstCredData,
		BuildRequestFn:  buildGstDigioRequest,
		ParseResponseFn: parseGstDigioResponse,
	})
}

func validateGstCredData(data json.RawMessage) error {
	var d GstCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("invalid GST cred_data: %w", err)
	}
	normalized := strings.ToUpper(strings.TrimSpace(d.IDNo))
	if !gstPattern.MatchString(normalized) {
		return fmt.Errorf("%w: invalid GST format", ErrInvalidCredID)
	}
	return nil
}

func buildGstDigioRequest(data json.RawMessage) (any, error) {
	var d GstCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return map[string]string{
		"id_no": strings.ToUpper(strings.TrimSpace(d.IDNo)),
	}, nil
}

func parseGstDigioResponse(body []byte) (*VerificationResult, error) {
	var resp gstDigioResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse GST response: %w", err)
	}

	if resp.ErrorMessage != "" {
		return &VerificationResult{
			Success: false,
			Error:   resp.ErrorMessage,
		}, nil
	}

	credData := map[string]any{
		"corporate_name": resp.CorporateName,
		"gstin":          resp.GSTIN,
		"details":        resp.Details,
	}

	return &VerificationResult{
		Success:      true,
		CredID:       resp.GSTIN,
		VerifiedData: credData,
	}, nil
}
