package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"credential-service/internal/service/client"
)

// Confirmed against team_pipeline_2807.py's verify_udyam(), which independently
// calls this same live Digio endpoint (also under the /client/v4/apis/... base,
// like FSSAI — not PAN/GST's /v3/client/kyc/... path).
const udyamEndpoint = "/client/v4/apis/kyc/fetch_id_data/UDYAMAADHAAR"

// Canonical hyphenated form, e.g. UDYAM-MH-01-1234567.
var udyamHyphenatedPattern = regexp.MustCompile(`^UDYAM-[A-Z]{2}-\d{2}-\d{7}$`)

// Compact form with no separators, e.g. UDYAMMH011234567.
var udyamCompactPattern = regexp.MustCompile(`^UDYAM[A-Z]{2}\d{2}\d{7}$`)

type UdyamCredData struct {
	IDNo string `json:"id_no"`
}

func NewUdyamHandler(digioClient *client.DigioClient) Verifier {
	return NewDigioVerifier(DigioVerifierConfig{
		Client:          digioClient,
		Endpoint:        udyamEndpoint,
		CredType:        "UDYAM",
		ValidateFn:      validateUdyamCredData,
		BuildRequestFn:  buildUdyamDigioRequest,
		ParseResponseFn: parseUdyamDigioResponse,
	})
}

func normalizeUdyam(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func udyamCompactForm(normalized string) string {
	return strings.ReplaceAll(strings.ReplaceAll(normalized, "-", ""), " ", "")
}

func validateUdyamCredData(data json.RawMessage) error {
	var d UdyamCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("invalid Udyam cred_data: %w", err)
	}
	normalized := normalizeUdyam(d.IDNo)
	if udyamHyphenatedPattern.MatchString(normalized) {
		return nil
	}
	if udyamCompactPattern.MatchString(udyamCompactForm(normalized)) {
		return nil
	}
	return fmt.Errorf("%w: invalid Udyam format, expected UDYAM-XX-00-0000000", ErrInvalidCredID)
}

// toCanonicalUdyam normalizes either accepted input form to the canonical
// hyphenated UDYAM-XX-00-0000000 shape before sending to Digio. Per
// docs/IMPLEMENTATION_ROADMAP.md Phase 8, this replaces trying both formats
// against Digio in a loop (as team_pipeline_2807.py does) — Digio's known
// flakiness on this endpoint is instead absorbed by the existing retry path.
func toCanonicalUdyam(raw string) string {
	normalized := normalizeUdyam(raw)
	if udyamHyphenatedPattern.MatchString(normalized) {
		return normalized
	}
	compact := udyamCompactForm(normalized)
	if udyamCompactPattern.MatchString(compact) {
		return fmt.Sprintf("%s-%s-%s-%s", compact[0:5], compact[5:7], compact[7:9], compact[9:16])
	}
	return normalized
}

func buildUdyamDigioRequest(data json.RawMessage) (any, error) {
	var d UdyamCredData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return map[string]string{"id_no": toCanonicalUdyam(d.IDNo)}, nil
}

// parseUdyamDigioResponse is a generic-map decode, not a struct, because
// Digio's Udyam response uses inconsistent/fallback key names for the same
// field (e.g. "Udyam Registration Number" vs "UAN"; "National Industry
// Classification Code(S)" vs "...Code") — mirroring the dict.get() fallback
// chains in team_pipeline_2807.py's verify_udyam().
func parseUdyamDigioResponse(body []byte) (*VerificationResult, error) {
	var d map[string]any
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("failed to parse Udyam response: %w", err)
	}

	uan, _ := d["Udyam Registration Number"].(string)
	if uan == "" {
		uan, _ = d["UAN"].(string)
	}
	if uan == "" {
		// Digio is known to flap on this endpoint: the same valid Udyam
		// number sometimes comes back 200 with no UAN. Report as a soft
		// failure so it goes through the retry path rather than failing
		// permanently on the first attempt.
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

	credData := map[string]any{
		"uan":                 uan,
		"enterprise_type":     enterpriseType,
		"classification_date": classificationDate,
		"name_of_enterprise":  nameOfEnterprise,
		"major_activity":      majorActivity,
		"nic_2_digit":         nic2Digit,
	}
	return &VerificationResult{
		Success:      true,
		CredID:       uan,
		VerifiedData: credData,
	}, nil
}
