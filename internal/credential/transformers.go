package credential

import "encoding/json"

// RequestTransformer builds a Digio request payload from validated cred_data.
// normalizedID is the id_no after definition normalization (trim/upper/remove_chars).
type RequestTransformer func(data json.RawMessage, normalizedID string) (any, error)

// ResponseTransformer parses a Digio HTTP 2xx body into a VerificationResult.
type ResponseTransformer func(body []byte) (*VerificationResult, error)

// requestTransformers is the named request transformer registry.
var requestTransformers = map[string]RequestTransformer{
	"udyam_canonicalize": udyamCanonicalize,
}

// responseTransformers is the named response transformer registry.
var responseTransformers = map[string]ResponseTransformer{
	"gst_parse":   gstParse,
	"fssai_parse": fssaiParse,
	"udyam_parse": udyamParse,
}

// KnownRequestTransformerNames returns names for loader validation.
func KnownRequestTransformerNames() map[string]struct{} {
	out := make(map[string]struct{}, len(requestTransformers))
	for k := range requestTransformers {
		out[k] = struct{}{}
	}
	return out
}

// KnownResponseTransformerNames returns names for loader validation.
func KnownResponseTransformerNames() map[string]struct{} {
	out := make(map[string]struct{}, len(responseTransformers))
	for k := range responseTransformers {
		out[k] = struct{}{}
	}
	return out
}
