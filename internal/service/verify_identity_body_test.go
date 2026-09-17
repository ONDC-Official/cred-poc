package service

// Internal test: bodyText is unexported, and the rest of the service suite lives in
// package service_test.

import "testing"

func TestBodyTextKeepsBytesExactly(t *testing.T) {
	t.Parallel()

	// Key order and spacing must survive; that is why the column is text, not jsonb.
	for _, body := range []string{
		`{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe"}`,
		`{"name": "John Doe",   "cred_id":"ABCDE1234F"}`,
		"{\n  \"cred_id\": \"ABCDE1234F\"\n}",
		`{"note":"symbols & <angles> stay put"}`,
	} {
		got := bodyText([]byte(body))
		if got == nil {
			t.Fatalf("expected a stored value for %q", body)
		}
		if *got != body {
			t.Fatalf("bytes changed\n got: %s\nwant: %s", *got, body)
		}
	}
}

func TestBodyTextKeepsNonJSONAsSent(t *testing.T) {
	t.Parallel()

	body := `<html>not json</html>`

	got := bodyText([]byte(body))
	if got == nil || *got != body {
		t.Fatalf("expected %q stored unchanged, got %v", body, got)
	}
}

func TestBodyTextReturnsNilForEmptyBody(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{nil, {}} {
		if got := bodyText(body); got != nil {
			t.Fatalf("expected nil for an empty body, got %q", *got)
		}
	}
}
