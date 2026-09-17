package handlers

import (
	"errors"

	"credential-service/internal/handlers/dto"
	"credential-service/internal/service"
	"credential-service/internal/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type CredentialHandler struct {
	service *service.CredentialService
}

func NewCredentialHandler(svc *service.CredentialService) *CredentialHandler {
	return &CredentialHandler{service: svc}
}

func (h *CredentialHandler) SubmitCredentials(c *fiber.Ctx) error {
	var req dto.SubmitCredentialsRequest
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

	resp, err := h.service.SubmitCredentials(c.UserContext(), &req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusAccepted).JSON(resp)
}

func (h *CredentialHandler) GetResults(c *fiber.Ctx) error {
	requestIDParam := c.Query("request_id")
	if requestIDParam == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "request_id is required",
		})
	}

	requestID, err := uuid.Parse(requestIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request_id",
		})
	}

	verbose := c.Query("verbose") == "true"
	resp, err := h.service.GetResults(c.UserContext(), requestID, verbose)
	if err != nil {
		if errors.Is(err, service.ErrRequestNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}
