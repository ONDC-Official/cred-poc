package handlers

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"credential-service/internal/models"
	"credential-service/internal/repository"

	"github.com/gofiber/fiber/v2"
)

const (
	defaultLogLimit  = 50
	maxLogLimit      = 200
	defaultLogWindow = 24 * time.Hour
	maxLogWindow     = 31 * 24 * time.Hour
)

// SubscriberLogHandler serves GET /subscriber/:id: one subscriber's /verify-identity
// calls, newest first, in pages.
type SubscriberLogHandler struct {
	repo      *repository.CredentialRequestRepository
	enumCache *models.EnumCache
}

func NewSubscriberLogHandler(
	repo *repository.CredentialRequestRepository,
	enumCache *models.EnumCache,
) *SubscriberLogHandler {
	return &SubscriberLogHandler{repo: repo, enumCache: enumCache}
}

type SubscriberLogItem struct {
	ID           string          `json:"id"`
	RequestBody  json.RawMessage `json:"request_body,omitempty"`
	ResponseBody json.RawMessage `json:"response_body,omitempty"`
	CredType     string          `json:"cred_type,omitempty"`
	Status       string          `json:"status,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type SubscriberLogResponse struct {
	SubscriberID string              `json:"subscriber_id"`
	Page         int                 `json:"page"`
	Limit        int                 `json:"limit"`
	Total        int64               `json:"total"`
	TotalPages   int                 `json:"total_pages"`
	Items        []SubscriberLogItem `json:"items"`
}

// List returns one page of a subscriber's calls. An unknown subscriber gets an empty
// list rather than a 404, which would confirm which subscriber ids exist.
func (h *SubscriberLogHandler) List(c *fiber.Ctx) error {
	if h == nil || h.repo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "subscriber logs are unavailable: no database configured",
		})
	}

	subscriberID := subscriberIDParam(c)
	if subscriberID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "subscriber id is required",
		})
	}

	filter, page, errMessage := h.buildFilter(c, subscriberID)
	if errMessage != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": errMessage})
	}

	total, err := h.repo.CountBySubscriber(c.UserContext(), filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to count subscriber logs",
		})
	}

	records, err := h.repo.ListBySubscriber(c.UserContext(), filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to read subscriber logs",
		})
	}

	return c.JSON(SubscriberLogResponse{
		SubscriberID: subscriberID,
		Page:         page,
		Limit:        filter.Limit,
		Total:        total,
		TotalPages:   totalPages(total, filter.Limit),
		Items:        h.buildItems(records),
	})
}

// buildFilter returns the filter, the page number, and a message for the first invalid
// parameter.
func (h *SubscriberLogHandler) buildFilter(c *fiber.Ctx, subscriberID string) (repository.SubscriberLogFilter, int, string) {
	to := time.Now().UTC()
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return repository.SubscriberLogFilter{}, 0, "to must be an RFC3339 timestamp"
		}
		to = parsed.UTC()
	}

	from := to.Add(-defaultLogWindow)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return repository.SubscriberLogFilter{}, 0, "from must be an RFC3339 timestamp"
		}
		from = parsed.UTC()
	}

	if !to.After(from) {
		return repository.SubscriberLogFilter{}, 0, "to must be after from"
	}
	if to.Sub(from) > maxLogWindow {
		return repository.SubscriberLogFilter{}, 0, "time range must not exceed 31 days"
	}

	limit := defaultLogLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxLogLimit {
			return repository.SubscriberLogFilter{}, 0, "limit must be a number between 1 and 200"
		}
		limit = parsed
	}

	page := 1
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return repository.SubscriberLogFilter{}, 0, "page must be a number greater than 0"
		}
		page = parsed
	}

	return repository.SubscriberLogFilter{
		SubscriberID: subscriberID,
		From:         from,
		To:           to,
		Limit:        limit,
		Offset:       (page - 1) * limit,
	}, page, ""
}

func totalPages(total int64, limit int) int {
	if total == 0 || limit <= 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

func (h *SubscriberLogHandler) buildItems(records []models.CredentialRequest) []SubscriberLogItem {
	items := make([]SubscriberLogItem, 0, len(records))

	for _, rec := range records {
		item := SubscriberLogItem{
			ID:           rec.ID.String(),
			RequestBody:  storedBody(rec.RequestBody),
			ResponseBody: storedBody(rec.ResponseBody),
			CreatedAt:    rec.CreatedAt,
		}

		if h.enumCache != nil {
			if et := h.enumCache.Get(rec.CredType); et != nil {
				item.CredType = et.Value
			}
			if et := h.enumCache.Get(rec.VerificationStatus); et != nil {
				item.Status = et.Value
			}
			if rec.Verifier != nil {
				if et := h.enumCache.Get(*rec.Verifier); et != nil {
					item.Provider = et.Value
				}
			}
		}

		item.Error = storedErrorMessage(rec.VerificationErrors)

		items = append(items, item)
	}

	return items
}

// storedBody returns the column verbatim. A body that never parsed is returned as a
// JSON string so one malformed record cannot invalidate the whole response.
func storedBody(body *string) json.RawMessage {
	if body == nil || *body == "" {
		return nil
	}

	raw := json.RawMessage(*body)
	if json.Valid(raw) {
		return raw
	}

	quoted, err := json.Marshal(*body)
	if err != nil {
		return nil
	}
	return quoted
}

func storedErrorMessage(verificationErrors *json.RawMessage) string {
	if verificationErrors == nil {
		return ""
	}

	var stored struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(*verificationErrors, &stored); err != nil {
		return ""
	}
	return stored.Error
}

// subscriberIDParam reads :id, unescaping it because ONDC ids are domain-like and may
// arrive percent-encoded.
func subscriberIDParam(c *fiber.Ctx) string {
	raw := c.Params("id")
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return strings.TrimSpace(raw)
}
