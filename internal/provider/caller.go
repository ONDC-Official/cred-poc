package provider

import "context"

// Caller is the transport used by a provider adapter (Digio today; others later).
// Auth, base URL, and timeouts stay inside the concrete client — not in YAML.
type Caller interface {
	Call(ctx context.Context, method, path string, payload any) (body []byte, statusCode int, err error)
}
