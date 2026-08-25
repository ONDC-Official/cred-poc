package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"credential-service/internal/service/client"
)

// FSSAI's endpoint lives under a different base path than PAN/GST's
// /v3/client/kyc/fetch_id_data/{TYPE} — confirmed against team_pipeline_2807.py's
// verify_fssai(), which independently calls this same live Digio endpoint.
const fssaiEndpoint = "/client/v4/apis/kyc/fetch_id_data/FSSAI"

var fssaiPattern = regexp.MustCompile(`^\d{14}$`)

type FssaiCredData struct {
	IDNo string `json:"id_no"`
}

// fssaiLegacyResponse is Digio's older, flat FSSAI response shape.
type fssaiLegacyResponse struct {
	LicenseNo       string `json:"License / Registration No."`
	Status          string `json:"Status"`
	PremisesAddress string `json:"Premises Address"`
	CompanyName     string `json:"Company Name"`
	ExpiryDate      string `json:"Expiry Date"`
	KindOfBusiness  string `json:"Kind of Business"`
}

// fssaiDetail is one entry of Digio's current fssai_details[] response shape.
type fssaiDetail struct {
	PremiseAddress      string `json:"premiseaddress"`
	LicenseNo           string `json:"licenseno"`
	LicenseCategoryName string `json:"licensecategoryname"`
	StateName           string `json:"statename"`
	LicenseActiveFlag   any    `json:"licenseactiveflag"` // Digio returns this as a bool or a string interchangeably
	StatusDesc          string `json:"statusdesc"`
	CompanyName         string `json:"companyname"`
}

type fssaiDetailsResponse struct {
	FssaiDetails []fssaiDetail `json:"fssai_details"`
}

func NewFssaiHandler(digioClient *client.DigioClient) Verifier {
	return NewDigioVerifier(DigioVerifierConfig{
		Client:          digioClient,
		Endpoint:        fssaiEndpoint,
		CredType:        "FSSAI",
		ValidateFn:      validateFssaiCredData,
		BuildRequestFn:  buildFssaiDigioRequest,
		ParseResponseFn: parseFssaiDigioResponse,
	})
}

func validateFssaiCredData(data json.RawMessage) error {
	var d FssaiCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("invalid FSSAI cred_data: %w", err)
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(d.IDNo), " ", "")
	if !fssaiPattern.MatchString(normalized) {
		return fmt.Errorf("%w: invalid FSSAI format, expected 14 digits", ErrInvalidCredID)
	}
	return nil
}

func buildFssaiDigioRequest(data json.RawMessage) (any, error) {
	var d FssaiCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(d.IDNo), " ", "")
	return map[string]string{"id_no": normalized}, nil
}

// fssaiActiveFlagString normalizes Digio's licenseactiveflag, which arrives
// as a JSON bool, a string, or is absent, into a single human-readable
// string — mirroring team_pipeline_2807.py's verify_fssai() normalization.
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

// parseFssaiDigioResponse handles both FSSAI response shapes Digio has been
// observed to return: a legacy flat object with a top-level "Status" field,
// and the current fssai_details[] array shape. Neither shape includes an
// explicit rejection code, so an unrecognized body is reported as a soft
// failure (Success: false, no Go error) rather than a hard error — the same
// treatment PAN/GST give an explicit Digio-side rejection.
func parseFssaiDigioResponse(body []byte) (*VerificationResult, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse FSSAI response: %w", err)
	}

	if _, ok := probe["Status"]; ok {
		var resp fssaiLegacyResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse FSSAI legacy response: %w", err)
		}
		credData := map[string]any{
			"license_no":       resp.LicenseNo,
			"status":           resp.Status,
			"premises_address": resp.PremisesAddress,
			"company_name":     resp.CompanyName,
			"expiry_date":      resp.ExpiryDate,
			"kind_of_business": resp.KindOfBusiness,
		}
		return &VerificationResult{
			Success:      true,
			CredID:       resp.LicenseNo,
			VerifiedData: credData,
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
	credData := map[string]any{
		"license_no":       det.LicenseNo,
		"status":           fssaiActiveFlagString(det.LicenseActiveFlag),
		"premises_address": det.PremiseAddress,
		"company_name":     det.CompanyName,
		"state":            det.StateName,
		"license_status":   det.StatusDesc,
		"license_category": det.LicenseCategoryName,
	}
	return &VerificationResult{
		Success:      true,
		CredID:       det.LicenseNo,
		VerifiedData: credData,
	}, nil
}
