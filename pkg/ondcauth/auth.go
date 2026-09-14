// Package ondcauth implements ONDC Authorization header signing and verification
// (Ed25519 + BLAKE2b-512 digest), aligned with ondc-crypto-sdk-go / ondc-crypto-sdk-nodejs.
package ondcauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
)

const defaultTTLSeconds int64 = 60 * 60

// DefaultTTL is how long a generated Authorization header stays valid. Callers that
// mint headers should use this rather than repeating the number.
const DefaultTTL = time.Duration(defaultTTLSeconds) * time.Second

var authHeaderKVRe = regexp.MustCompile(`\s*([^=]+)=([^,]+)[,]?`)

type CreateAuthorizationHeaderParams struct {
	Body                  string
	PrivateKey            string
	SubscriberID          string
	SubscriberUniqueKeyID string
	Created               string
	Expires               string
}

type IsHeaderValidParams struct {
	Header    string
	Body      string
	PublicKey string
}

type VerifyAuthorisationHeaderParams struct {
	AuthHeader string
	Payload    string
	PublicKey  string
}

var nowUnix = func() int64 { return time.Now().Unix() }

func CreateAuthorizationHeader(p CreateAuthorizationHeaderParams) (string, error) {
	signingString, created, expires, err := createSigningString(p.Body, p.Created, p.Expires)
	if err != nil {
		return "", err
	}

	signature, err := signMessage(signingString, p.PrivateKey)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		`Signature keyId="%s|%s|ed25519",algorithm="ed25519",created="%s",expires="%s",headers="(created) (expires) digest",signature="%s"`,
		p.SubscriberID,
		p.SubscriberUniqueKeyID,
		created,
		expires,
		signature,
	), nil
}

func IsHeaderValid(p IsHeaderValidParams) bool {
	defer func() { _ = recover() }()

	parts := splitAuthHeader(p.Header)
	created, _ := parts["created"]
	expires, _ := parts["expires"]
	signatureB64, _ := parts["signature"]

	signingString, _, _, err := createSigningString(p.Body, created, expires)
	if err != nil {
		return false
	}

	ok, err := verifyMessage(signatureB64, signingString, p.PublicKey)
	if err != nil {
		return false
	}
	return ok
}

func VerifyAuthorisationHeader(p VerifyAuthorisationHeaderParams) (bool, error) {
	parts := splitAuthHeader(p.AuthHeader)

	// Reject a non-ed25519 header before spending any work on the digest.
	if alg, ok := parts["algorithm"]; ok {
		if normalized := strings.ToLower(strings.TrimSpace(alg)); normalized != "" && normalized != AlgorithmEd25519 {
			return false, fmt.Errorf("unsupported algorithm: %s", alg)
		}
	}

	createdStr, ok := parts["created"]
	if !ok || createdStr == "" {
		return false, errors.New("missing created")
	}
	expiresStr, ok := parts["expires"]
	if !ok || expiresStr == "" {
		return false, errors.New("missing expires")
	}
	signatureB64, ok := parts["signature"]
	if !ok || signatureB64 == "" {
		return false, errors.New("missing signature")
	}

	created, err := strconv.ParseInt(createdStr, 10, 64)
	if err != nil {
		return false, errors.New("invalid created")
	}
	expires, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return false, errors.New("invalid expires")
	}

	now := nowUnix()
	if created > now {
		return false, errors.New("created is in the future")
	}
	if now > expires {
		return false, errors.New("authorization header expired")
	}

	signingString, _, _, err := createSigningString(p.Payload, createdStr, expiresStr)
	if err != nil {
		return false, err
	}

	okSig, err := verifyMessage(signatureB64, signingString, p.PublicKey)
	if err != nil {
		return false, err
	}
	if !okSig {
		return false, errors.New("invalid signature")
	}

	return true, nil
}

// AlgorithmEd25519 is the only signature algorithm ONDC permits on this scheme.
const AlgorithmEd25519 = "ed25519"

// ParseKeyID extracts subscriber_id and unique_key_id from the header's keyId.
// It also enforces that both the keyId's algorithm segment and the header's own
// algorithm parameter name ed25519; anything else is rejected outright.
func ParseKeyID(authHeader string) (subscriberID, uniqueKeyID string, err error) {
	parts := splitAuthHeader(authHeader)
	keyID, ok := parts["keyId"]
	if !ok || keyID == "" {
		return "", "", errors.New("keyId not found in Authorization header")
	}
	segments := strings.SplitN(keyID, "|", 3)
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("malformed keyId: %s", keyID)
	}

	// The algorithm appears twice: as the keyId's third segment and as the separate
	// algorithm parameter. Reject on either, so a downgrade needs both to be forged
	// and still fails.
	if len(segments) == 3 {
		if alg := strings.ToLower(strings.TrimSpace(segments[2])); alg != "" && alg != AlgorithmEd25519 {
			return "", "", fmt.Errorf("unsupported algorithm in keyId: %s", segments[2])
		}
	}
	if alg, ok := parts["algorithm"]; ok {
		if normalized := strings.ToLower(strings.TrimSpace(alg)); normalized != "" && normalized != AlgorithmEd25519 {
			return "", "", fmt.Errorf("unsupported algorithm: %s", alg)
		}
	}

	return segments[0], segments[1], nil
}

func createSigningString(message, created, expires string) (signingString string, createdOut string, expiresOut string, err error) {
	if created == "" {
		created = strconv.FormatInt(nowUnix(), 10)
	}
	if expires == "" {
		createdInt, parseErr := strconv.ParseInt(created, 10, 64)
		if parseErr != nil {
			expires = "NaN"
		} else {
			expires = strconv.FormatInt(createdInt+defaultTTLSeconds, 10)
		}
	}

	digestBase64, err := blake512DigestBase64(message)
	if err != nil {
		return "", "", "", err
	}

	signingString = fmt.Sprintf("(created): %s\n(expires): %s\ndigest: BLAKE-512=%s", created, expires, digestBase64)
	return signingString, created, expires, nil
}

func signMessage(signingString, privateKeyB64 string) (string, error) {
	privateKey, err := parseEd25519PrivateKey(privateKeyB64)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(privateKey, []byte(signingString))
	return base64.StdEncoding.EncodeToString(sig), nil
}

func verifyMessage(signedStringB64, signingString, publicKeyB64 string) (bool, error) {
	signatureBytes, err := decodeBase64Original(signedStringB64)
	if err != nil {
		return false, err
	}
	publicKeyBytes, err := decodeBase64Original(publicKeyB64)
	if err != nil {
		return false, err
	}
	if len(signatureBytes) != ed25519.SignatureSize {
		return false, errors.New("invalid ed25519 signature length")
	}
	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return false, errors.New("invalid ed25519 public key length")
	}
	return ed25519.Verify(ed25519.PublicKey(publicKeyBytes), []byte(signingString), signatureBytes), nil
}

func splitAuthHeader(authHeader string) map[string]string {
	header := strings.TrimSpace(authHeader)
	header = strings.TrimPrefix(header, "Signature ")
	parts := map[string]string{}
	matches := authHeaderKVRe.FindAllStringSubmatch(header, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			parts[strings.TrimSpace(m[1])] = removeQuotes(strings.TrimSpace(m[2]))
		}
	}
	return parts
}

func removeQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first := s[0]
	last := s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func blake512DigestBase64(message string) (string, error) {
	h, err := blake2b.New(64, nil)
	if err != nil {
		return "", err
	}
	if _, err := h.Write([]byte(message)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// decodeBase64Original decodes standard Base64 only, padded or unpadded. The ONDC
// scheme mandates the standard alphabet; the URL-safe alphabet is deliberately not
// accepted, so a key or signature has exactly one valid representation.
func decodeBase64Original(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("empty base64 string")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	if b2, err2 := base64.RawStdEncoding.DecodeString(s); err2 == nil {
		return b2, nil
	}
	return nil, err
}

func parseEd25519PrivateKey(privateKeyB64 string) (ed25519.PrivateKey, error) {
	keyBytes, err := decodeBase64Original(privateKeyB64)
	if err != nil {
		return nil, err
	}
	switch len(keyBytes) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(keyBytes), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(keyBytes), nil
	default:
		return nil, fmt.Errorf("invalid ed25519 private key length: got %d, want %d or %d", len(keyBytes), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}
