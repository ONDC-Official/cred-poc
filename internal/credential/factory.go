package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	credconfig "credential-service/internal/credential/config"
	"credential-service/internal/service/client"
)

type credDataEnvelope struct {
	IDNo string `json:"id_no"`
	Name string `json:"name"`
	Dob  string `json:"dob"`
}

// NewVerifierFromDefinition builds a DigioVerifier from a validated Definition.
func NewVerifierFromDefinition(def *credconfig.Definition, digioClient *client.DigioClient) (Verifier, error) {
	if def == nil {
		return nil, fmt.Errorf("definition is nil")
	}

	var reqXform RequestTransformer
	if name := strings.TrimSpace(def.Request.Transformer); name != "" {
		fn, ok := requestTransformers[name]
		if !ok {
			return nil, fmt.Errorf("unknown request transformer %q", name)
		}
		reqXform = fn
	}

	var respXform ResponseTransformer
	if name := strings.TrimSpace(def.Response.Transformer); name != "" {
		fn, ok := responseTransformers[name]
		if !ok {
			return nil, fmt.Errorf("unknown response transformer %q", name)
		}
		respXform = fn
	}

	return NewDigioVerifier(DigioVerifierConfig{
		Client:          digioClient,
		Endpoint:        def.Provider.Endpoint,
		CredType:        def.CredentialType,
		ValidateFn:      makeValidateFn(def),
		BuildRequestFn:  makeBuildRequestFn(def, reqXform),
		ParseResponseFn: makeParseResponseFn(def, respXform),
	}), nil
}

func makeValidateFn(def *credconfig.Definition) func(json.RawMessage) error {
	return func(data json.RawMessage) error {
		var d credDataEnvelope
		if err := json.Unmarshal(data, &d); err != nil {
			return fmt.Errorf("invalid %s cred_data: %w", def.CredentialType, err)
		}

		normalized := NormalizeID(d.IDNo, def.Normalization)
		if !MatchesAnyPattern(normalized, def.CompiledPatterns) {
			msg := fmt.Sprintf("invalid %s format", def.CredentialType)
			switch def.CredentialType {
			case "FSSAI":
				msg = "invalid FSSAI format, expected 14 digits"
			case "UDYAM":
				msg = "invalid Udyam format, expected UDYAM-XX-00-0000000"
			case "PAN":
				msg = "invalid PAN format"
			case "GST":
				msg = "invalid GST format"
			}
			return fmt.Errorf("%w: %s", ErrInvalidCredID, msg)
		}

		for _, field := range def.Validation.RequiredFields {
			switch field {
			case "name":
				if strings.TrimSpace(d.Name) == "" {
					return fmt.Errorf("%s cred_data: name is required", def.CredentialType)
				}
			case "dob":
				if strings.TrimSpace(d.Dob) == "" {
					return fmt.Errorf("%s cred_data: dob is required", def.CredentialType)
				}
			case "id_no":
				// already validated via patterns
			default:
				return fmt.Errorf("%s cred_data: unsupported required field %q", def.CredentialType, field)
			}
		}
		return nil
	}
}

func makeBuildRequestFn(def *credconfig.Definition, xform RequestTransformer) func(json.RawMessage) (any, error) {
	return func(data json.RawMessage) (any, error) {
		var d credDataEnvelope
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		normalized := NormalizeID(d.IDNo, def.Normalization)

		if xform != nil {
			return xform(data, normalized)
		}

		payload := make(map[string]string, len(def.Request.Fields))
		for digioKey, source := range def.Request.Fields {
			switch source {
			case credconfig.FieldSourceNormalizedID:
				payload[digioKey] = normalized
			case "name":
				payload[digioKey] = strings.TrimSpace(d.Name)
			case "dob":
				payload[digioKey] = strings.TrimSpace(d.Dob)
			case "id_no":
				payload[digioKey] = normalized
			default:
				return nil, fmt.Errorf("unknown request field source %q", source)
			}
		}
		return payload, nil
	}
}

func makeParseResponseFn(def *credconfig.Definition, xform ResponseTransformer) func([]byte) (*VerificationResult, error) {
	if xform != nil {
		return xform
	}
	return func(body []byte) (*VerificationResult, error) {
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", def.CredentialType, err)
		}

		if soft := strings.TrimSpace(def.Response.SoftFailField); soft != "" {
			if v, ok := raw[soft]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return &VerificationResult{Success: false, Error: s}, nil
				}
			}
		}

		verified := make(map[string]any, len(def.Response.Fields))
		for digioKey, outKey := range def.Response.Fields {
			verified[outKey] = raw[digioKey]
		}

		credID := ""
		if sf := strings.TrimSpace(def.Response.SuccessField); sf != "" {
			if v, ok := raw[sf]; ok {
				credID, _ = v.(string)
			}
		}

		return &VerificationResult{
			Success:      true,
			CredID:       credID,
			VerifiedData: verified,
		}, nil
	}
}

// NormalizeID applies definition normalization rules to a raw credential id.
func NormalizeID(raw string, n credconfig.Normalization) string {
	out := raw
	if n.Trim {
		out = strings.TrimSpace(out)
	}
	if n.Uppercase {
		out = strings.ToUpper(out)
	}
	if n.RemoveChars != "" {
		for _, r := range n.RemoveChars {
			out = strings.ReplaceAll(out, string(r), "")
		}
	}
	return out
}

// MatchesAnyPattern returns true if value matches any compiled pattern, or if
// the hyphen/space-stripped form matches (needed for UDYAM compact ids).
func MatchesAnyPattern(value string, patterns []*regexp.Regexp) bool {
	compact := strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), " ", "")
	for _, re := range patterns {
		if re.MatchString(value) || re.MatchString(compact) {
			return true
		}
	}
	return false
}
