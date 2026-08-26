package credential

import (
	"encoding/json"
	"fmt"
)

type gstDigioResponse struct {
	ErrorMessage  string         `json:"error_message"`
	CorporateName string         `json:"corporate_name"`
	GSTIN         string         `json:"gstin"`
	Details       map[string]any `json:"details"`
}

func gstParse(body []byte) (*VerificationResult, error) {
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

	return &VerificationResult{
		Success: true,
		CredID:  resp.GSTIN,
		VerifiedData: map[string]any{
			"corporate_name": resp.CorporateName,
			"gstin":          resp.GSTIN,
			"details":        resp.Details,
		},
	}, nil
}
