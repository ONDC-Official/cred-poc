package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// MockClient is a deterministic stub implementing provider.Caller. It makes
// no network call and always succeeds — used as a fallback provider so the
// multi-provider chain can be exercised end-to-end without a second real
// KYC vendor.
type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

// Call ignores method/path and returns a canned success body shaped for the
// fetch_id_data_pan capability, echoing back the requested id_no as "pan".
func (c *MockClient) Call(_ context.Context, _, _ string, payload any) ([]byte, int, error) {
	idNo := ""
	if m, ok := payload.(map[string]string); ok {
		idNo = m["id_no"]
	}

	body, err := json.Marshal(map[string]string{
		"pan":       idNo,
		"category":  "Individual",
		"status":    "VALID",
		"full_name": "Mock Provider Response",
	})
	if err != nil {
		return nil, 0, err
	}
	return body, http.StatusOK, nil
}
