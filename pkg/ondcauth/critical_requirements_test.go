package ondcauth_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"credential-service/pkg/ondcauth"

	"golang.org/x/crypto/blake2b"
)

// The tests in this file pin the cryptographic contract the ONDC scheme requires.
// Each one maps to a single stated requirement, so a change that breaks one of them
// fails loudly rather than silently producing headers no ONDC peer can verify.

const reqBody = `{"cred_type":"PAN","cred_id":"ABCDE1234F"}`

func mustHeader(t *testing.T, body, privB64 string) string {
	t.Helper()
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            privB64,
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}
	return header
}

func headerField(t *testing.T, header, field string) string {
	t.Helper()
	for _, part := range strings.Split(strings.TrimPrefix(header, "Signature "), ",") {
		k, v, found := strings.Cut(part, "=")
		if found && strings.TrimSpace(k) == field {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	t.Fatalf("field %q not found in header %q", field, header)
	return ""
}

// Requirement: hash with BLAKE2b at 512-bit (64 byte) output, not BLAKE2s.
func TestRequirementDigestIsBlake2b512(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	header := mustHeader(t, reqBody, base64.StdEncoding.EncodeToString(priv))

	// Recompute the digest independently and confirm the signature is over it.
	h, err := blake2b.New(64, nil)
	if err != nil {
		t.Fatalf("blake2b: %v", err)
	}
	if _, err := h.Write([]byte(reqBody)); err != nil {
		t.Fatalf("write digest: %v", err)
	}
	sum := h.Sum(nil)
	if len(sum) != 64 {
		t.Fatalf("expected a 64-byte digest, got %d bytes", len(sum))
	}

	created := headerField(t, header, "created")
	expires := headerField(t, header, "expires")
	signingString := "(created): " + created + "\n(expires): " + expires +
		"\ndigest: BLAKE-512=" + base64.StdEncoding.EncodeToString(sum)

	sig, err := base64.StdEncoding.DecodeString(headerField(t, header, "signature"))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), []byte(signingString), sig) {
		t.Fatal("signature is not over a BLAKE2b-512 digest of the exact body")
	}
}

// Requirement: sign and verify with Ed25519, not RSA or ECDSA.
func TestRequirementAlgorithmIsEd25519Only(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	header := mustHeader(t, reqBody, base64.StdEncoding.EncodeToString(priv))

	if got := headerField(t, header, "algorithm"); got != "ed25519" {
		t.Fatalf("algorithm = %q, want ed25519", got)
	}
	if keyID := headerField(t, header, "keyId"); !strings.HasSuffix(keyID, "|ed25519") {
		t.Fatalf("keyId = %q, want an ed25519 suffix", keyID)
	}

	for _, alg := range []string{"rsa", "ecdsa", "hmac-sha256"} {
		forged := strings.Replace(header, `algorithm="ed25519"`, `algorithm="`+alg+`"`, 1)
		if _, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
			AuthHeader: forged,
			Payload:    reqBody,
			PublicKey:  base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		}); err == nil {
			t.Fatalf("algorithm %q was accepted", alg)
		}
	}
}

// Requirement: hash the exact payload string; never parse and re-stringify the JSON.
func TestRequirementPayloadIsUsedVerbatim(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	// Signed with this exact spacing and key order.
	signed := `{"b":2,  "a":1}`
	header := mustHeader(t, signed, base64.StdEncoding.EncodeToString(priv))

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header, Payload: signed, PublicKey: pubB64,
	})
	if err != nil || !ok {
		t.Fatalf("the exact signed bytes must verify, got ok=%v err=%v", ok, err)
	}

	// Semantically identical JSON, different bytes. Must NOT verify: proof that the
	// raw string is hashed rather than a re-serialization.
	for _, equivalent := range []string{`{"a":1,"b":2}`, `{"b":2,"a":1}`, `{"b": 2, "a": 1}`} {
		ok, _ := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
			AuthHeader: header, Payload: equivalent, PublicKey: pubB64,
		})
		if ok {
			t.Fatalf("re-serialized payload %q verified; the raw bytes are not being hashed", equivalent)
		}
	}
}

// Requirement: accept a 32-byte seed and a 64-byte expanded private key, and produce
// the same signature from either.
func TestRequirementBothPrivateKeyLengthsWork(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	seedB64 := base64.StdEncoding.EncodeToString(priv.Seed()) // 32 bytes
	fullB64 := base64.StdEncoding.EncodeToString(priv)        // 64 bytes

	if len(priv.Seed()) != 32 || len(priv) != 64 {
		t.Fatalf("unexpected key sizes: seed=%d full=%d", len(priv.Seed()), len(priv))
	}

	now := time.Now().Unix()
	created := strconv.FormatInt(now, 10)
	expires := strconv.FormatInt(now+3600, 10)

	signatures := make(map[string]string, 2)
	for name, key := range map[string]string{"32-byte seed": seedB64, "64-byte key": fullB64} {
		header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
			Body:                  reqBody,
			PrivateKey:            key,
			SubscriberID:          "np.example.com",
			SubscriberUniqueKeyID: "key-001",
			Created:               created,
			Expires:               expires,
		})
		if err != nil {
			t.Fatalf("%s: create header: %v", name, err)
		}
		ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
			AuthHeader: header, Payload: reqBody, PublicKey: pubB64,
		})
		if err != nil || !ok {
			t.Fatalf("%s: expected the header to verify, got ok=%v err=%v", name, ok, err)
		}
		signatures[name] = headerField(t, header, "signature")
	}

	// A seed and its expanded key are the same key, so over identical created/expires
	// they must produce byte-identical signatures.
	if signatures["32-byte seed"] != signatures["64-byte key"] {
		t.Fatal("seed and expanded private key produced different signatures")
	}

	// Anything other than 32 or 64 bytes must be refused outright.
	if _, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:       reqBody,
		PrivateKey: base64.StdEncoding.EncodeToString(make([]byte, 48)),
	}); err == nil {
		t.Fatal("a 48-byte private key was accepted")
	}
}

// Requirement: emit standard Base64, never the URL-safe alphabet.
func TestRequirementBase64IsStandardNotURLSafe(t *testing.T) {
	t.Parallel()

	// A seed chosen so its digest and signature exercise the bytes that differ between
	// the two alphabets: standard uses + and /, URL-safe uses - and _.
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i * 7)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pubB64 := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))

	header := mustHeader(t, reqBody, base64.StdEncoding.EncodeToString(seed))
	signature := headerField(t, header, "signature")

	if strings.ContainsAny(signature, "-_") {
		t.Fatalf("signature %q uses the URL-safe alphabet", signature)
	}
	if _, err := base64.StdEncoding.DecodeString(signature); err != nil {
		t.Fatalf("signature is not standard Base64: %v", err)
	}

	// A URL-safe public key must be rejected rather than silently re-interpreted.
	urlSafeKey := base64.URLEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	if urlSafeKey != pubB64 {
		if ok, _ := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
			AuthHeader: header, Payload: reqBody, PublicKey: urlSafeKey,
		}); ok {
			t.Fatal("a URL-safe encoded public key was accepted")
		}
	}
}

// Requirement: reject headers outside their created/expires window.
func TestRequirementCreatedAndExpiresAreEnforced(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	privB64 := base64.StdEncoding.EncodeToString(priv)

	cases := map[string]struct{ created, expires string }{
		"expired":           {"1000000000", "1000003600"},
		"created in future": {"4000000000", "4000003600"},
	}

	for name, tc := range cases {
		header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
			Body:                  reqBody,
			PrivateKey:            privB64,
			SubscriberID:          "np.example.com",
			SubscriberUniqueKeyID: "key-001",
			Created:               tc.created,
			Expires:               tc.expires,
		})
		if err != nil {
			t.Fatalf("%s: create header: %v", name, err)
		}
		// The signature itself is valid; only the time window makes it unacceptable.
		if _, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
			AuthHeader: header, Payload: reqBody, PublicKey: pubB64,
		}); err == nil {
			t.Fatalf("%s: header outside its validity window was accepted", name)
		}
	}
}
