package credential

import (
	"encoding/json"
	"fmt"
)

func udyamParse(body []byte) (*VerificationResult, error) {
	var d map[string]any
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("failed to parse Udyam response: %w", err)
	}

	uan, _ := d["Udyam Registration Number"].(string)
	if uan == "" {
		uan, _ = d["UAN"].(string)
	}
	if uan == "" {
		return &VerificationResult{
			Success: false,
			Error:   "digio returned no Udyam Registration Number (known Digio flakiness on this endpoint)",
		}, nil
	}

	var enterpriseType, classificationDate string
	if etList, ok := d["Enterprise Type"].([]any); ok && len(etList) > 0 {
		if first, ok := etList[0].(map[string]any); ok {
			enterpriseType, _ = first["Enterprise Type"].(string)
			classificationDate, _ = first["Classification Date"].(string)
		}
	} else if s, ok := d["Enterprise Type"].(string); ok {
		enterpriseType = s
	}

	nicCodesRaw, ok := d["National Industry Classification Code(S)"]
	if !ok {
		nicCodesRaw = d["National Industry Classification Code"]
	}
	var nic2Digit string
	if nicCodes, ok := nicCodesRaw.([]any); ok && len(nicCodes) > 0 {
		if first, ok := nicCodes[0].(map[string]any); ok {
			nic2Digit, _ = first["Nic 2 Digit"].(string)
			if nic2Digit == "" {
				nic2Digit, _ = first["NIC 2 Digit"].(string)
			}
		}
	}

	nameOfEnterprise, _ := d["Name of Enterprise"].(string)
	majorActivity, _ := d["Major Activity"].(string)

	return &VerificationResult{
		Success: true,
		CredID:  uan,
		VerifiedData: map[string]any{
			"uan":                 uan,
			"enterprise_type":     enterpriseType,
			"classification_date": classificationDate,
			"name_of_enterprise":  nameOfEnterprise,
			"major_activity":      majorActivity,
			"nic_2_digit":         nic2Digit,
		},
	}, nil
}
