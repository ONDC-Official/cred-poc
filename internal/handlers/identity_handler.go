package handlers

import (
	"encoding/json"
	"errors"

	"credential-service/internal/credential"
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
}

func NewIdentityHandler(svc *service.IdentityService) *IdentityHandler {
	return &IdentityHandler{service: svc}
}

func (h *IdentityHandler) VerifyIdentity(c *fiber.Ctx) error {
	var req VerifyIdentityRequest
	if err := c.BodyParser(&req); err != nil {
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

	result, err := h.service.VerifyIdentity(c.Context(), req.CredType, credData)
	if err != nil {
		if errors.Is(err, credential.ErrUnsupportedCredType) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// No evidences means validateFn rejected the input before any provider
	// call (e.g. a missing required field).
	if len(result.Evidences) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": result.Error,
		})
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
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
					"error": "provider response evidence has unexpected type",
				})
			}
			// Process()'s statusCode>=400 branch never validates the body as
			// JSON (the async evidences pipeline only needs the raw bytes) —
			// but embedding it here as json.RawMessage requires valid JSON.
			if !json.Valid(raw) {
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
					"error": "provider returned a non-JSON response body",
				})
			}
			resp.ProviderResponse = raw
			break
		}
	}

	return c.JSON(resp)
}
