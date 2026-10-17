package http

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// alertIngestPrefix is the one path family isPublicPath lets through on the
// strength of a URL token. The token is a credential, so RedactPath keeps it
// out of every log line that records a path.
const alertIngestPrefix = "/v1/alerts/ingest/"

const maxAlertIngestBody = 256 << 10

func (h *Handler) registerAlertTokenRoutes(app fiber.Router) {
	app.Post("/v1/repositories/:id/alert-tokens", h.CreateAlertToken)
	app.Get("/v1/repositories/:id/alert-tokens", h.ListAlertTokens)
	app.Delete("/v1/repositories/:id/alert-tokens/:tokenId", h.DeleteAlertToken)
	app.Post(alertIngestPrefix+":token", h.IngestAlertWithToken)
}

func isAlertIngestPath(path string) bool {
	rest, ok := strings.CutPrefix(path, alertIngestPrefix)
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// RedactPath is what to log in place of c.Path(): identical except for an
// alert ingest path, whose token segment is replaced.
func RedactPath(path string) string {
	if strings.HasPrefix(strings.ToLower(path), alertIngestPrefix) {
		return alertIngestPrefix + "[redacted]"
	}
	return path
}

type alertTokenView struct {
	ID         uuid.UUID  `json:"id"`
	Label      string     `json:"label"`
	DefaultEnv string     `json:"default_env"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type createdAlertTokenView struct {
	ID         uuid.UUID `json:"id"`
	Label      string    `json:"label"`
	DefaultEnv string    `json:"default_env"`
	CreatedAt  time.Time `json:"created_at"`
	Token      string    `json:"token"`
	IngestPath string    `json:"ingest_path"`
}

func viewAlertToken(t domain.AlertIngestToken) alertTokenView {
	return alertTokenView{ID: t.ID, Label: t.Label, DefaultEnv: t.DefaultEnv, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt}
}

// CreateAlertToken — POST /v1/repositories/:id/alert-tokens
// The plaintext token is in this response and nowhere else, ever.
func (h *Handler) CreateAlertToken(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Label      string `json:"label"`
		DefaultEnv string `json:"default_env"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	tok, plaintext, err := h.prodOpsSvc.CreateAlertToken(h.enrichContext(c), id, req.Label, req.DefaultEnv)
	switch {
	case errors.Is(err, prodops.ErrInvalidAlertToken):
		return badRequest(c, err.Error())
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, "repository not found")
	case errors.Is(err, prodops.ErrAlertTokensUnavailable):
		return alertTokensUnavailable(c)
	case err != nil:
		return internalError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(createdAlertTokenView{
		ID:         tok.ID,
		Label:      tok.Label,
		DefaultEnv: tok.DefaultEnv,
		CreatedAt:  tok.CreatedAt,
		Token:      plaintext,
		IngestPath: alertIngestPrefix + plaintext,
	})
}

// ListAlertTokens — GET /v1/repositories/:id/alert-tokens
func (h *Handler) ListAlertTokens(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	tokens, err := h.prodOpsSvc.ListAlertTokens(h.enrichContext(c), id)
	if errors.Is(err, prodops.ErrAlertTokensUnavailable) {
		return alertTokensUnavailable(c)
	}
	if err != nil {
		return internalError(c, err)
	}
	out := make([]alertTokenView, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, viewAlertToken(t))
	}
	return c.JSON(fiber.Map{"tokens": out})
}

// DeleteAlertToken — DELETE /v1/repositories/:id/alert-tokens/:tokenId
func (h *Handler) DeleteAlertToken(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	id, err := uuid.Parse(c.Params("tokenId"))
	if err != nil {
		return badRequest(c, "invalid alert token id")
	}
	err = h.prodOpsSvc.DeleteAlertToken(h.enrichContext(c), repositoryID, id)
	switch {
	case errors.Is(err, prodops.ErrAlertTokenNotFound):
		return notFound(c, "alert token not found")
	case errors.Is(err, prodops.ErrAlertTokensUnavailable):
		return alertTokensUnavailable(c)
	case err != nil:
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func alertTokensUnavailable(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: prodops.ErrAlertTokensUnavailable.Error(), Type: "service_unavailable"},
	})
}

func alertRateLimited(c *fiber.Ctx) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate_limited"})
}

// IngestAlertWithToken — POST /v1/alerts/ingest/:token
// The same ingest as POST /v1/repositories/:id/incidents, for a caller that
// holds a token rather than the API key. Its answers say nothing about the
// repository or the incident beyond the id, and an unknown token is a bare 404.
//
// Fasthttp has already read the body (up to the app-wide limit) by the time a
// handler runs, so the Content-Length check only spares the parse and the
// lookup; the length check after it catches a chunked body.
func (h *Handler) IngestAlertWithToken(c *fiber.Ctx) error {
	if c.Request().Header.ContentLength() > maxAlertIngestBody || len(c.Body()) > maxAlertIngestBody {
		return c.SendStatus(fiber.StatusRequestEntityTooLarge)
	}
	ctx := h.enrichContext(c)
	tok, err := h.prodOpsSvc.AdmitAlertToken(ctx, c.Params("token"))
	switch {
	case errors.Is(err, prodops.ErrAlertTokenNotFound):
		return c.SendStatus(fiber.StatusNotFound)
	case errors.Is(err, prodops.ErrAlertRateLimited):
		return alertRateLimited(c)
	case err != nil:
		log.Error().Err(err).Msg("alert ingest: token lookup failed")
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	def := prodops.Defaults{
		Env:    firstNonEmptyString(strings.TrimSpace(c.Query("env")), tok.DefaultEnv),
		Source: strings.TrimSpace(c.Query("source")),
	}
	incident, err := h.ingestAlert(c, def, func(ctx context.Context, in domain.IncidentInput) (domain.Incident, error) {
		return h.prodOpsSvc.IngestWithAlertToken(ctx, tok, in)
	})
	switch {
	case isAlertPayloadError(err):
		return badRequest(c, err.Error())
	case errors.Is(err, prodops.ErrAlertRateLimited):
		return alertRateLimited(c)
	case err != nil:
		log.Error().Err(err).Str("alert_token_id", tok.ID.String()).Msg("alert ingest failed")
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	h.prodOpsSvc.MarkAlertTokenUsed(ctx, tok.ID)
	if incident.ID == uuid.Nil {
		return c.SendStatus(fiber.StatusAccepted)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"incident_id": incident.ID})
}

// alertPayloadError marks a Normalize failure so each route can answer it with
// a 400 carrying Normalize's own message.
type alertPayloadError struct{ err error }

func (e alertPayloadError) Error() string { return e.err.Error() }
func (e alertPayloadError) Unwrap() error { return e.err }

func isAlertPayloadError(err error) bool {
	var target alertPayloadError
	return errors.As(err, &target)
}

// ingestAlert is the normalize step both alert routes share before their own
// ingest. A zero incident ID means a recovery for an unknown fingerprint: a no-op.
func (h *Handler) ingestAlert(c *fiber.Ctx, def prodops.Defaults, ingest func(context.Context, domain.IncidentInput) (domain.Incident, error)) (domain.Incident, error) {
	in, err := prodops.Normalize(c.Body(), def)
	if err != nil {
		return domain.Incident{}, alertPayloadError{err: err}
	}
	return ingest(h.enrichContext(c), in)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
