package credential

import (
	"encoding/json"
	"fmt"
	"strings"
)

type fssaiLegacyResponse struct {
	LicenseNo       string `json:"License / Registration No."`
	Status          string `json:"Status"`
	PremisesAddress string `json:"Premises Address"`
	CompanyName     string `json:"Company Name"`
	ExpiryDate      string `json:"Expiry Date"`
	KindOfBusiness  string `json:"Kind of Business"`
}

type fssaiDetail struct {
	PremiseAddress      string `json:"premiseaddress"`
	LicenseNo           string `json:"licenseno"`
	LicenseCategoryName string `json:"licensecategoryname"`
	StateName           string `json:"statename"`
	LicenseActiveFlag   any    `json:"licenseactiveflag"`
	StatusDesc          string `json:"statusdesc"`
	CompanyName         string `json:"companyname"`
}

type fssaiDetailsResponse struct {
	FssaiDetails []fssaiDetail `json:"fssai_details"`
}

func fssaiParse(body []byte) (*VerificationResult, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse FSSAI response: %w", err)
	}

	if _, ok := probe["Status"]; ok {
		var resp fssaiLegacyResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse FSSAI legacy response: %w", err)
		}
		return &VerificationResult{
			Success: true,
			CredID:  resp.LicenseNo,
			VerifiedData: map[string]any{
				"license_no":       resp.LicenseNo,
				"status":           resp.Status,
				"premises_address": resp.PremisesAddress,
				"company_name":     resp.CompanyName,
				"expiry_date":      resp.ExpiryDate,
				"kind_of_business": resp.KindOfBusiness,
			},
		}, nil
	}

	var resp fssaiDetailsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse FSSAI response: %w", err)
	}
	if len(resp.FssaiDetails) == 0 {
		return &VerificationResult{
			Success: false,
			Error:   "unexpected FSSAI response format: no fssai_details entries",
		}, nil
	}

	det := resp.FssaiDetails[0]
	return &VerificationResult{
		Success: true,
		CredID:  det.LicenseNo,
		VerifiedData: map[string]any{
			"license_no":       det.LicenseNo,
			"status":           fssaiActiveFlagString(det.LicenseActiveFlag),
			"premises_address": det.PremiseAddress,
			"company_name":     det.CompanyName,
			"state":            det.StateName,
			"license_status":   det.StatusDesc,
			"license_category": det.LicenseCategoryName,
		},
	}, nil
}

func fssaiActiveFlagString(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "Active"
		}
		return "Inactive"
	case string:
		return strings.TrimSpace(t)
	default:
		return ""
	}
}
