package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"credential-service/pkg/ondcauth"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultLookupTimeout  = 10 * time.Second
	defaultLookupCacheTTL = 5 * time.Minute
	// statusSubscribed is the only registry status whose keys may authenticate a caller.
	statusSubscribed = "SUBSCRIBED"
)

// RegistryClient resolves a subscriber's Ed25519 signing public key.
// The interface exists so the verifier can be tested without a network.
type RegistryClient interface {
	LookupSubscriberKey(ctx context.Context, subscriberID, ukid string) (string, error)
}

type ONDCRegistryClientConfig struct {
	// LookupURL is the ONDC Registry /lookup endpoint.
	LookupURL string
	// PrivateKey, SubscriberID and UniqueKeyID are this service's own credentials,
	// used to sign the outbound lookup call.
	PrivateKey   string
	SubscriberID string
	UniqueKeyID  string
	// CacheTTL is how long a resolved key is reused. Zero selects the default.
	CacheTTL time.Duration
	// Timeout bounds a single lookup call. Ignored when HTTPClient is supplied.
	Timeout time.Duration
	// HTTPClient is injectable for tests.
	HTTPClient *http.Client
}

type ONDCRegistryClient struct {
	lookupURL    string
	privateKey   string
	subscriberID string
	uniqueKeyID  string
	httpClient   *http.Client

	cacheTTL time.Duration
	mu       sync.RWMutex
	cache    map[string]cachedKey
}

type cachedKey struct {
	publicKey string
	expiresAt time.Time
}

// RegistrySubscriberKey is one entry of the /lookup response array.
// The registry spells the key id as either ukId or unique_key_id depending on
// version, so both are decoded and either may match.
type RegistrySubscriberKey struct {
	SubscriberID     string `json:"subscriber_id"`
	Status           string `json:"status"`
	UKID             string `json:"ukId"`
	UniqueKeyID      string `json:"unique_key_id"`
	SubscriberURL    string `json:"subscriber_url"`
	SigningPublicKey string `json:"signing_public_key"`
	EncrPublicKey    string `json:"encr_public_key"`
	ValidFrom        string `json:"valid_from"`
	ValidUntil       string `json:"valid_until"`
	Type             string `json:"type"`
}

func NewONDCRegistryClient(cfg ONDCRegistryClientConfig) *ONDCRegistryClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultLookupTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		// Client spans and http.client.* metrics, with trace context propagated to the ONDC
		// Registry. An injected HTTPClient is used as given.
		client = &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultLookupCacheTTL
	}
	return &ONDCRegistryClient{
		lookupURL:    cfg.LookupURL,
		privateKey:   cfg.PrivateKey,
		subscriberID: cfg.SubscriberID,
		uniqueKeyID:  cfg.UniqueKeyID,
		httpClient:   client,
		cacheTTL:     ttl,
		cache:        make(map[string]cachedKey),
	}
}

// LookupSubscriberKey returns the signing_public_key registered for subscriberID
// under the key id ukid. It never returns an arbitrary or first-listed key: the
// entry must match the subscriber exactly, carry the requested key id, and be
// SUBSCRIBED and within its validity window.
func (c *ONDCRegistryClient) LookupSubscriberKey(ctx context.Context, subscriberID, ukid string) (string, error) {
	if c.lookupURL == "" {
		return "", fmt.Errorf("ONDC Registry lookup URL is not configured")
	}
	if subscriberID == "" || ukid == "" {
		return "", fmt.Errorf("subscriber_id and unique_key_id are both required for registry lookup")
	}

	cacheKey := subscriberID + "|" + ukid
	if pub, ok := c.cachedKey(cacheKey); ok {
		return pub, nil
	}

	keys, err := c.fetch(ctx, subscriberID, ukid)
	if err != nil {
		return "", err
	}

	now := time.Now()
	for _, key := range keys {
		if !matches(key, subscriberID, ukid) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(key.Status), statusSubscribed) {
			slog.WarnContext(ctx, "registry lookup: skipping key that is not SUBSCRIBED",
				"subscriber_id", subscriberID, "ukid", ukid, "status", key.Status)
			continue
		}
		if !withinValidity(key, now) {
			slog.WarnContext(ctx, "registry lookup: skipping key outside its validity window",
				"subscriber_id", subscriberID, "ukid", ukid, "valid_from", key.ValidFrom, "valid_until", key.ValidUntil)
			continue
		}
		pubKey := strings.TrimSpace(key.SigningPublicKey)
		if pubKey == "" {
			continue
		}
		c.storeKey(cacheKey, pubKey)
		slog.InfoContext(ctx, "registry lookup: resolved signing key", "subscriber_id", subscriberID, "ukid", ukid)
		return pubKey, nil
	}

	return "", fmt.Errorf("no usable signing_public_key for subscriber_id %q and unique_key_id %q in ONDC Registry", subscriberID, ukid)
}

func (c *ONDCRegistryClient) fetch(ctx context.Context, subscriberID, ukid string) ([]RegistrySubscriberKey, error) {
	payload := map[string]string{
		"country":       "IND",
		"subscriber_id": subscriberID,
		"ukId":          ukid,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal lookup payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.lookupURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("create lookup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// The registry requires the lookup itself to be signed with this service's own
	// credentials. Same signing function the inbound path verifies with.
	if c.privateKey != "" {
		authHeader, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
			Body:                  string(payloadBytes),
			PrivateKey:            c.privateKey,
			SubscriberID:          c.subscriberID,
			SubscriberUniqueKeyID: c.uniqueKeyID,
		})
		if err != nil {
			return nil, fmt.Errorf("sign registry lookup request: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ONDC Registry lookup call failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read registry lookup response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// The body is only worth logging when the call failed.
		return nil, fmt.Errorf("ONDC Registry lookup returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var keys []RegistrySubscriberKey
	if err := json.Unmarshal(bodyBytes, &keys); err != nil {
		// The registry answers a miss with HTTP 200 and a NACK envelope rather than an
		// array, so decode that shape to report why instead of a JSON type error.
		if nack, ok := decodeNACK(bodyBytes); ok {
			return nil, fmt.Errorf("ONDC Registry rejected the lookup: %s", nack)
		}
		return nil, fmt.Errorf("parse registry lookup response: %w", err)
	}
	return keys, nil
}

// registryNACK is the error envelope the registry returns in place of the key array.
type registryNACK struct {
	Message struct {
		Ack struct {
			Status string `json:"status"`
		} `json:"ack"`
	} `json:"message"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeNACK(body []byte) (string, bool) {
	var nack registryNACK
	if err := json.Unmarshal(body, &nack); err != nil {
		return "", false
	}
	if nack.Error.Message == "" && nack.Error.Code == "" && nack.Message.Ack.Status == "" {
		return "", false
	}
	detail := strings.TrimSpace(nack.Error.Message)
	if detail == "" {
		detail = strings.TrimSpace(nack.Message.Ack.Status)
	}
	if code := strings.TrimSpace(nack.Error.Code); code != "" {
		return fmt.Sprintf("%s (code %s)", detail, code), true
	}
	return detail, true
}

// matches requires an exact subscriber match and the requested key id on either
// of the two field spellings. Prefix or empty-value matching would let the
// registry hand back a key for a different participant.
func matches(key RegistrySubscriberKey, subscriberID, ukid string) bool {
	if strings.TrimSpace(key.SubscriberID) != subscriberID {
		return false
	}
	return strings.TrimSpace(key.UKID) == ukid || strings.TrimSpace(key.UniqueKeyID) == ukid
}

// withinValidity enforces valid_from/valid_until when the registry populates them.
// An absent or unparseable bound is treated as open rather than as a rejection,
// since the registry is inconsistent about the format.
func withinValidity(key RegistrySubscriberKey, now time.Time) bool {
	if from, ok := parseRegistryTime(key.ValidFrom); ok && now.Before(from) {
		return false
	}
	if until, ok := parseRegistryTime(key.ValidUntil); ok && now.After(until) {
		return false
	}
	return true
}

func parseRegistryTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05.000", "2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (c *ONDCRegistryClient) cachedKey(cacheKey string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[cacheKey]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.publicKey, true
}

func (c *ONDCRegistryClient) storeKey(cacheKey, publicKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[cacheKey] = cachedKey{publicKey: publicKey, expiresAt: time.Now().Add(c.cacheTTL)}
}
