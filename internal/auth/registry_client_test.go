package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"credential-service/internal/auth"
)

func testKeyPair(t *testing.T) (pubB64, privB64 string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv)
}

// registryStub serves a fixed body and counts how many times it was called.
func registryStub(t *testing.T, status int, body string, calls *int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRegistryClientResolvesKeyForMatchingUKID(t *testing.T) {
	t.Parallel()

	pubB64, privB64 := testKeyPair(t)
	// Two entries for the same subscriber: only the second carries the requested ukid.
	body := `[
		{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"other-key","signing_public_key":"d3Jvbmcta2V5"},
		{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"key-001","signing_public_key":"` + pubB64 + `"}
	]`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{
		LookupURL:    server.URL,
		PrivateKey:   privB64,
		SubscriberID: "self.example.com",
		UniqueKeyID:  "self-key-1",
	})

	got, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != pubB64 {
		t.Fatalf("expected the key matching ukid key-001, got %q", got)
	}
}

func TestRegistryClientSignsItsOwnLookupRequest(t *testing.T) {
	t.Parallel()

	pubB64, privB64 := testKeyPair(t)

	var gotAuth string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"key-001","signing_public_key":"` + pubB64 + `"}]`))
	}))
	t.Cleanup(server.Close)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{
		LookupURL:    server.URL,
		PrivateKey:   privB64,
		SubscriberID: "self.example.com",
		UniqueKeyID:  "self-key-1",
	})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(gotAuth, "Signature ") {
		t.Fatalf("lookup request was not signed, Authorization = %q", gotAuth)
	}
	for _, want := range []string{`keyId="self.example.com|self-key-1|ed25519"`, `algorithm="ed25519"`, "signature="} {
		if !strings.Contains(gotAuth, want) {
			t.Fatalf("signed header missing %s, got %q", want, gotAuth)
		}
	}
	if !strings.Contains(string(gotBody), `"subscriber_id":"np.example.com"`) {
		t.Fatalf("lookup payload missing subscriber_id, got %q", string(gotBody))
	}
}

func TestRegistryClientRejectsUnknownUKID(t *testing.T) {
	t.Parallel()

	body := `[{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"different-key","signing_public_key":"c29tZS1rZXk="}]`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "wanted-key"); err == nil {
		t.Fatal("expected an error when no entry carries the requested ukid")
	}
}

func TestRegistryClientRejectsSubscriberMismatch(t *testing.T) {
	t.Parallel()

	// Same ukid, but registered to a different subscriber. A prefix or empty match
	// would wrongly accept this.
	body := `[{"subscriber_id":"np.example.com.attacker.test","status":"SUBSCRIBED","ukId":"key-001","signing_public_key":"c29tZS1rZXk="}]`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err == nil {
		t.Fatal("expected an error when the entry belongs to a different subscriber")
	}
}

func TestRegistryClientRejectsUnsubscribedStatus(t *testing.T) {
	t.Parallel()

	body := `[{"subscriber_id":"np.example.com","status":"REVOKED","ukId":"key-001","signing_public_key":"c29tZS1rZXk="}]`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err == nil {
		t.Fatal("expected an error for a key whose status is not SUBSCRIBED")
	}
}

func TestRegistryClientRejectsExpiredKey(t *testing.T) {
	t.Parallel()

	body := `[{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"key-001",
		"signing_public_key":"c29tZS1rZXk=","valid_from":"2020-01-01T00:00:00Z","valid_until":"2021-01-01T00:00:00Z"}]`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err == nil {
		t.Fatal("expected an error for a key past its valid_until")
	}
}

func TestRegistryClientRejectsNon200Response(t *testing.T) {
	t.Parallel()

	server := registryStub(t, http.StatusInternalServerError, `{"error":"boom"}`, nil)
	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	_, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001")
	if err == nil {
		t.Fatal("expected an error for a non-200 registry response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected the status in the error, got %v", err)
	}
}

func TestRegistryClientRejectsMalformedResponse(t *testing.T) {
	t.Parallel()

	server := registryStub(t, http.StatusOK, `not json at all`, nil)
	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err == nil {
		t.Fatal("expected an error for a malformed registry response")
	}
}

func TestRegistryClientCachesResolvedKey(t *testing.T) {
	t.Parallel()

	pubB64, _ := testKeyPair(t)
	var calls int32
	body := `[{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"key-001","signing_public_key":"` + pubB64 + `"}]`
	server := registryStub(t, http.StatusOK, body, &calls)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{
		LookupURL: server.URL,
		CacheTTL:  time.Minute,
	})

	for i := 0; i < 3; i++ {
		got, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if got != pubB64 {
			t.Fatalf("call %d: unexpected key %q", i, got)
		}
	}

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("expected 1 registry round trip across 3 lookups, got %d", n)
	}
}

func TestRegistryClientRequiresLookupURL(t *testing.T) {
	t.Parallel()

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{})
	if _, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001"); err == nil {
		t.Fatal("expected an error when no lookup URL is configured")
	}
}

func TestRegistryClientReportsNACKEnvelope(t *testing.T) {
	t.Parallel()

	// The registry answers an unknown subscriber with HTTP 200 and this envelope
	// rather than the usual array.
	body := `{"message":{"ack":{"status":"NACK"}},"error":{"code":"15045","message":"No matching network participant found. Unrecognized values for: subscriber_id"}}`
	server := registryStub(t, http.StatusOK, body, nil)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{LookupURL: server.URL})

	_, err := client.LookupSubscriberKey(context.Background(), "attacker.example.com", "key-001")
	if err == nil {
		t.Fatal("expected an error for a NACK response")
	}
	if !strings.Contains(err.Error(), "No matching network participant") {
		t.Fatalf("expected the registry's reason in the error, got %v", err)
	}
	if !strings.Contains(err.Error(), "15045") {
		t.Fatalf("expected the registry's error code in the error, got %v", err)
	}
}

func TestRegistryClientConcurrentLookups(t *testing.T) {
	t.Parallel()

	pubB64, _ := testKeyPair(t)
	var calls int32
	body := `[{"subscriber_id":"np.example.com","status":"SUBSCRIBED","ukId":"key-001","signing_public_key":"` + pubB64 + `"}]`
	server := registryStub(t, http.StatusOK, body, &calls)

	client := auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{
		LookupURL: server.URL,
		CacheTTL:  time.Minute,
	})

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := client.LookupSubscriberKey(context.Background(), "np.example.com", "key-001")
			if err != nil {
				errs <- err
				return
			}
			if got != pubB64 {
				errs <- fmt.Errorf("unexpected key %q", got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent lookup failed: %v", err)
	}
}
