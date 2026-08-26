package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	credconfig "credential-service/internal/credential/config"
	"credential-service/internal/service/client"
)

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
		fields, err := parseCredDataFields(data)
		if err != nil {
			return fmt.Errorf("invalid %s cred_data: %w", def.CredentialType, err)
		}

		for fieldName, rule := range def.Validation.Fields {
			value := fieldValue(fields, fieldName, def)

			if rule.Required && value == "" {
				return fmt.Errorf("%s cred_data: %s is required", def.CredentialType, fieldName)
			}
			// Optional fields with patterns: skip when empty.
			if value == "" || len(rule.CompiledPatterns) == 0 {
				continue
			}
			if !MatchesAnyPattern(value, rule.CompiledPatterns) {
				msg := strings.TrimSpace(rule.Message)
				if msg == "" {
					msg = fmt.Sprintf("invalid %s format", fieldName)
				}
				return fmt.Errorf("%w: %s", ErrInvalidCredID, msg)
			}
		}
		return nil
	}
}

func makeBuildRequestFn(def *credconfig.Definition, xform RequestTransformer) func(json.RawMessage) (any, error) {
	return func(data json.RawMessage) (any, error) {
		fields, err := parseCredDataFields(data)
		if err != nil {
			return nil, err
		}
		normalized := NormalizeID(fields["id_no"], def.Normalization)

		if xform != nil {
			return xform(data, normalized)
		}

		payload := make(map[string]string, len(def.Request.Fields))
		for digioKey, source := range def.Request.Fields {
			switch source {
			case credconfig.FieldSourceNormalizedID, "id_no":
				payload[digioKey] = normalized
			default:
				// Any other source is read by cred_data key name (e.g. name, dob).
				payload[digioKey] = strings.TrimSpace(fields[source])
			}
		}
		return payload, nil
	}
}

func parseCredDataFields(data json.RawMessage) (map[string]string, error) {
	var fields map[string]string
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]string{}
	}
	return fields, nil
}

// fieldValue returns the cred_data value for fieldName, applying definition
// normalization only to id_no (the credential identifier).
func fieldValue(fields map[string]string, fieldName string, def *credconfig.Definition) string {
	raw := fields[fieldName]
	if fieldName == "id_no" {
		return NormalizeID(raw, def.Normalization)
	}
	return strings.TrimSpace(raw)
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
