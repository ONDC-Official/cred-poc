package handlers

import (
	"strconv"
	"strings"
	"time"

	"credential-service/pkg/ondcauth"

	"github.com/gofiber/fiber/v2"
)

// GenerateHeaderResponse is what POST /generate-header returns.
type GenerateHeaderResponse struct {
	// Authorization is the header value to copy into the real request.
	Authorization string `json:"authorization"`
	// Payload echoes the exact bytes that were signed. The request you send next
	// must carry these bytes verbatim or the signature will not verify.
	Payload string `json:"payload"`
	// SubscriberID and UniqueKeyID are the identity the header was signed as.
	SubscriberID string `json:"subscriber_id"`
	UniqueKeyID  string `json:"unique_key_id"`
	Created      string `json:"created"`
	Expires      string `json:"expires"`
	// ExpiresAt is Expires in a form a human can read, because an expired header is
	// the most common reason a signed request is rejected.
	ExpiresAt    string `json:"expires_at"`
	ValidForSecs int64  `json:"valid_for_seconds"`
}

// AuthHandler serves the developer helper that mints Authorization headers.
//
// It signs whatever bytes it is given with this service's own private key, so it is
// registered outside the protected route group (you need it to produce the header that
// group demands) and only when explicitly enabled. Never expose it publicly.
type AuthHandler struct {
	privateKey   string
	subscriberID string
	uniqueKeyID  string
}

type AuthHandlerConfig struct {
	PrivateKey   string
	SubscriberID string
	UniqueKeyID  string
}

func NewAuthHandler(cfg AuthHandlerConfig) *AuthHandler {
	return &AuthHandler{
		privateKey:   strings.TrimSpace(cfg.PrivateKey),
		subscriberID: strings.TrimSpace(cfg.SubscriberID),
		uniqueKeyID:  strings.TrimSpace(cfg.UniqueKeyID),
	}
}

// GenerateHeader signs the raw request body and returns an Authorization header for it.
//
// The body you POST here is the body you send next: the signature covers the exact
// bytes, so the two must match byte for byte. Send an empty body to sign a GET.
//
// ?subscriber_id= and ?unique_key_id= override the configured identity, which is useful
// for testing how the service reacts to an unregistered caller. The private key is never
// accepted over HTTP; only the configured one is used.
func (h *AuthHandler) GenerateHeader(c *fiber.Ctx) error {
	if h.privateKey == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "CREDENTIAL_SERVICE_AUTH_SIGNING_PRIVATE is not configured, so no header can be signed",
		})
	}

	subscriberID := h.subscriberID
	if override := strings.TrimSpace(c.Query("subscriber_id")); override != "" {
		subscriberID = override
	}
	uniqueKeyID := h.uniqueKeyID
	if override := strings.TrimSpace(c.Query("unique_key_id")); override != "" {
		uniqueKeyID = override
	}

	if subscriberID == "" || uniqueKeyID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "subscriber_id and unique_key_id must come from CREDENTIAL_SERVICE_AUTH_SUBSCRIBER_ID / _UNIQUE_KEY_ID or from the query string",
		})
	}

	// Sign the raw bytes, never a re-serialization: the caller has to send these exact
	// bytes, so parsing and re-encoding here would silently invalidate the signature.
	payload := string(c.Body())

	created := time.Now().Unix()
	expires := created + int64(ondcauth.DefaultTTL/time.Second)

	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  payload,
		PrivateKey:            h.privateKey,
		SubscriberID:          subscriberID,
		SubscriberUniqueKeyID: uniqueKeyID,
		Created:               strconv.FormatInt(created, 10),
		Expires:               strconv.FormatInt(expires, 10),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "could not sign the payload: " + err.Error(),
		})
	}

	// Plain-text mode returns the header and nothing else. JSON escapes every quote in
	// the header as \", and copying that out of a response viewer produces a header the
	// service then rejects. Text mode removes that trap entirely, so it is the form to
	// use when a human is going to copy and paste.
	if wantsPlainText(c) {
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
		// Expiry still matters to the reader, but it must not pollute the value being
		// copied, so it goes in a response header rather than the body.
		c.Set("X-Header-Expires-At", time.Unix(expires, 0).UTC().Format(time.RFC3339))
		return c.SendString(header)
	}

	return c.JSON(GenerateHeaderResponse{
		Authorization: header,
		Payload:       payload,
		SubscriberID:  subscriberID,
		UniqueKeyID:   uniqueKeyID,
		Created:       strconv.FormatInt(created, 10),
		Expires:       strconv.FormatInt(expires, 10),
		ExpiresAt:     time.Unix(expires, 0).UTC().Format(time.RFC3339),
		ValidForSecs:  expires - created,
	})
}

// wantsPlainText reports whether the caller asked for the bare header string, either
// with ?format=text or an Accept header naming text/plain.
func wantsPlainText(c *fiber.Ctx) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("format"))) {
	case "text", "txt", "plain", "raw", "header":
		return true
	}
	accept := strings.ToLower(c.Get(fiber.HeaderAccept))
	return strings.Contains(accept, "text/plain")
}
