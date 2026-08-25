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
	Name     string `json:"name" validate:"required_if=CredType PAN"`
	Dob      string `json:"dob" validate:"required_if=CredType PAN"`
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

	// No evidences means validateFn rejected the input before any Digio call
	// (e.g. invalid PAN/GST format) — a client error, not an upstream one.
	if len(result.Evidences) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": result.Error,
		})
	}

	for _, e := range result.Evidences {
		if e.Type != "response" {
			continue
		}
		raw, ok := e.Data.(json.RawMessage)
		if !ok {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "digio response evidence has unexpected type",
			})
		}
		// Process()'s statusCode>=400 branch never validates the body as
		// JSON (the async evidences pipeline only needs the raw bytes) —
		// but this endpoint must still guarantee it returns valid JSON.
		if !json.Valid(raw) {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "digio returned a non-JSON response body",
			})
		}
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		return c.Send(raw)
	}

	return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
		"error": "digio response evidence missing",
	})
}
