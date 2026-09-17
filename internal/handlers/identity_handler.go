package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"credential-service/internal/credential"
	appmiddleware "credential-service/internal/middleware"
	"credential-service/internal/service"
	"credential-service/internal/utils"

	"github.com/gofiber/fiber/v2"
)

type VerifyIdentityRequest struct {
	// ParticipantID is the onboarded NP id from the registry service DB.
	// Not required for Verify Identity for now; uncomment if/when needed.
	// ParticipantID string `json:"participant_id" validate:"required"`
	CredID   string `json:"cred_id" validate:"required"`
	CredType string `json:"cred_type" validate:"required"`
	Name     string `json:"name,omitempty"`
	Dob      string `json:"dob,omitempty"`
}

type VerifyIdentityResponse struct {
	Success  bool           `json:"success"`
	Provider string         `json:"provider,omitempty"`
	CredID   string         `json:"cred_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Error    string         `json:"error,omitempty"`
	// ProviderResponse is only populated when ?verbose=true is set.
	ProviderResponse json.RawMessage `json:"provider_response,omitempty"`
}

type IdentityHandler struct {
	service *service.IdentityService
	// logger is nil when no database is configured.
	logger *service.VerifyIdentityLogger
}

func NewIdentityHandler(svc *service.IdentityService, logger *service.VerifyIdentityLogger) *IdentityHandler {
	return &IdentityHandler{service: svc, logger: logger}
}

func (h *IdentityHandler) VerifyIdentity(c *fiber.Ctx) error {
	var req VerifyIdentityRequest
	if err := c.BodyParser(&req); err != nil {
		// Not recorded: an unparseable body names no cred_type to reference.
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid JSON",
		})
	}

	if err := utils.Validate(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	credData, err := json.Marshal(map[string]string{
		"id_no": req.CredID,
		"name":  req.Name,
		"dob":   req.Dob,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to build request",
		})
	}

	result, err := h.service.VerifyIdentity(c.UserContext(), req.CredType, credData)
	if err != nil {
		if errors.Is(err, credential.ErrUnsupportedCredType) {
			// Not recorded: no enum row exists for this type.
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return h.respond(c, fiber.StatusBadGateway, fiber.Map{"error": err.Error()},
			&service.VerifyIdentityLogEntry{
				CredType: req.CredType,
				CredData: credData,
				Error:    err.Error(),
			})
	}

	entry := &service.VerifyIdentityLogEntry{
		CredType: req.CredType,
		CredData: credData,
		Result:   result,
	}

	// No evidences means validateFn rejected the input before any provider
	// call (e.g. a missing required field).
	if len(result.Evidences) == 0 {
		return h.respond(c, fiber.StatusBadRequest, fiber.Map{"error": result.Error}, entry)
	}

	resp := VerifyIdentityResponse{
		Success:  result.Success,
		Provider: result.Provider,
		CredID:   result.CredID,
		Data:     result.VerifiedData,
		Error:    result.Error,
	}

	if c.Query("verbose") == "true" {
		for _, e := range result.Evidences {
			if e.Type != "response" {
				continue
			}
			raw, ok := e.Data.(json.RawMessage)
			if !ok {
				return h.respond(c, fiber.StatusBadGateway,
					fiber.Map{"error": "provider response evidence has unexpected type"}, entry)
			}
			// Process()'s statusCode>=400 branch never validates the body as
			// JSON (the async evidences pipeline only needs the raw bytes) —
			// but embedding it here as json.RawMessage requires valid JSON.
			if !json.Valid(raw) {
				return h.respond(c, fiber.StatusBadGateway,
					fiber.Map{"error": "provider returned a non-JSON response body"}, entry)
			}
			resp.ProviderResponse = raw
			break
		}
	}

	return h.respond(c, fiber.StatusOK, resp, entry)
}

// respond writes the response, then records the call with the bytes on both sides. A
// write failure is logged and changes nothing the caller sees.
func (h *IdentityHandler) respond(c *fiber.Ctx, status int, payload any, entry *service.VerifyIdentityLogEntry) error {
	if err := c.Status(status).JSON(payload); err != nil {
		return err
	}

	if h.logger == nil || entry == nil {
		return nil
	}

	// fasthttp reuses both buffers once this handler returns, so copy before the
	// goroutine reads them.
	entry.SubscriberID = appmiddleware.ClaimedIdentity(c).SubscriberID
	entry.RequestBody = copyBytes(c.Body())
	entry.ResponseBody = copyBytes(c.Response().Body())

	// WithoutCancel keeps the insert inside this request's trace without it being cancelled
	// when the request ends a moment later.
	ctx := context.WithoutCancel(c.UserContext())
	recorded := *entry
	go func() {
		if err := h.logger.Record(ctx, recorded); err != nil {
			slog.ErrorContext(ctx, "verify-identity log write failed",
				"subscriber_id", recorded.SubscriberID, "error", err)
		}
	}()

	return nil
}

func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}
