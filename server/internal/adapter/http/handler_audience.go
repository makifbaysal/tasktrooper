package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	audienceNoRuntimeCode = "audience_no_runtime"
	audienceBusyCode      = "audience_busy"
	audienceCallCode      = "audience_call_failed"
)

func (h *Handler) registerAudienceRoutes(app fiber.Router) {
	if h.audienceSvc == nil {
		return
	}
	app.Get("/v1/audience-research/status", h.GetAudienceRuntime)

	app.Get("/v1/audiences", h.ListAudiences)
	app.Post("/v1/audiences", h.CreateAudience)
	app.Get("/v1/audiences/:id", h.GetAudience)
	app.Patch("/v1/audiences/:id", h.UpdateAudience)
	app.Delete("/v1/audiences/:id", h.DeleteAudience)
	app.Post("/v1/audiences/:id/build", h.BuildAudience)
	app.Post("/v1/audiences/:id/build/cancel", h.CancelAudienceBuild)
	app.Post("/v1/audiences/:id/studies", h.PlanAudienceStudy)

	app.Get("/v1/audience-studies", h.ListAudienceStudies)
	app.Get("/v1/audience-studies/:id", h.GetAudienceStudy)
	app.Patch("/v1/audience-studies/:id", h.UpdateAudienceStudy)
	app.Delete("/v1/audience-studies/:id", h.DeleteAudienceStudy)
	app.Post("/v1/audience-studies/:id/start", h.StartAudienceStudy)
	app.Post("/v1/audience-studies/:id/cancel", h.CancelAudienceStudy)
	app.Post("/v1/audience-studies/:id/ask", h.AskAudienceResearch)
	app.Post("/v1/audience-studies/:id/ask-everyone", h.AskAudienceEveryone)
	app.Post("/v1/audience-studies/:id/personas/:persona/follow-up", h.FollowUpAudiencePersona)

	app.Get("/v1/audience-personas/:id/messages", h.GetAudienceThread)
	app.Post("/v1/audience-personas/:id/messages", h.SendAudienceMessage)
	app.Delete("/v1/audience-personas/:id/messages", h.ClearAudienceThread)
}

func audienceCoded(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"code": code, "error": message})
}

// audienceFailure maps the service's refusals to the answers the screen tells
// apart; anything else is an internal error.
func audienceFailure(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrAudienceNotFound), errors.Is(err, domain.ErrStudyNotFound), errors.Is(err, domain.ErrPersonaNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, audience.ErrNoRuntime):
		return audienceCoded(c, fiber.StatusConflict, audienceNoRuntimeCode, err.Error())
	case errors.Is(err, audience.ErrBusy):
		return audienceCoded(c, fiber.StatusConflict, audienceBusyCode, err.Error())
	case errors.Is(err, domain.ErrAudienceInvalid):
		return badRequest(c, strings.TrimPrefix(err.Error(), domain.ErrAudienceInvalid.Error()+": "))
	case errors.Is(err, audience.ErrCallFailed):
		log.Warn().Err(err).Msg("audience: model call failed")
		return audienceCoded(c, fiber.StatusBadGateway, audienceCallCode, strings.TrimPrefix(err.Error(), audience.ErrCallFailed.Error()+": "))
	}
	return internalError(c, err)
}

func audienceID(c *fiber.Ctx, param string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(param))
	if err != nil {
		return uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	return id, nil
}

func (h *Handler) GetAudienceRuntime(c *fiber.Ctx) error {
	out, err := h.audienceSvc.Runtime(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) ListAudiences(c *fiber.Ctx) error {
	out, err := h.audienceSvc.ListAudiences(h.enrichContext(c))
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(fiber.Map{"audiences": out})
}

type audienceRequest struct {
	Name          *string               `json:"name"`
	Repository    *string               `json:"repository"`
	Brief         *domain.AudienceBrief `json:"brief"`
	PersonaTarget *int                  `json:"persona_target"`
}

func (h *Handler) CreateAudience(c *fiber.Ctx) error {
	var req audienceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	in := audience.CreateAudienceInput{}
	if req.Name != nil {
		in.Name = *req.Name
	}
	if req.Repository != nil {
		in.Repository = *req.Repository
	}
	if req.Brief != nil {
		in.Brief = *req.Brief
	}
	if req.PersonaTarget != nil {
		in.PersonaTarget = *req.PersonaTarget
	}
	out, err := h.audienceSvc.CreateAudience(h.enrichContext(c), in)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *Handler) GetAudience(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	out, err := h.audienceSvc.Audience(h.enrichContext(c), id)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) UpdateAudience(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req audienceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.audienceSvc.UpdateAudience(h.enrichContext(c), id, audience.UpdateAudienceInput{
		Name: req.Name, Repository: req.Repository, Brief: req.Brief, PersonaTarget: req.PersonaTarget,
	})
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) DeleteAudience(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	if err := h.audienceSvc.DeleteAudience(h.enrichContext(c), id); err != nil {
		return audienceFailure(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) BuildAudience(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	out, err := h.audienceSvc.Build(h.enrichContext(c), id)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(out)
}

func (h *Handler) CancelAudienceBuild(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	if err := h.audienceSvc.CancelBuild(h.enrichContext(c), id); err != nil {
		return audienceFailure(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type studyRequest struct {
	Title       string    `json:"title"`
	Goal        string    `json:"goal"`
	Questions   []string  `json:"questions"`
	PricePoints []float64 `json:"price_points"`
	Currency    string    `json:"currency"`
	Question    string    `json:"question"`
}

func (r studyRequest) plan() audience.PlanStudyInput {
	return audience.PlanStudyInput{Title: r.Title, Goal: r.Goal, Questions: r.Questions, PricePoints: r.PricePoints, Currency: r.Currency}
}

func (h *Handler) PlanAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req studyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.audienceSvc.PlanStudy(h.enrichContext(c), id, req.plan())
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *Handler) ListAudienceStudies(c *fiber.Ctx) error {
	var filter *uuid.UUID
	if raw := c.Query("audience_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid audience_id")
		}
		filter = &id
	}
	out, err := h.audienceSvc.ListStudies(h.enrichContext(c), filter)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(fiber.Map{"studies": out})
}

func (h *Handler) GetAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	out, err := h.audienceSvc.Study(h.enrichContext(c), id)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) UpdateAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req studyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.audienceSvc.UpdateStudy(h.enrichContext(c), id, req.plan())
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) DeleteAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	if err := h.audienceSvc.DeleteStudy(h.enrichContext(c), id); err != nil {
		return audienceFailure(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) StartAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	out, err := h.audienceSvc.StartStudy(h.enrichContext(c), id)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(out)
}

func (h *Handler) CancelAudienceStudy(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	if err := h.audienceSvc.CancelStudy(h.enrichContext(c), id); err != nil {
		return audienceFailure(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AskAudienceResearch(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req studyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	answer, err := h.audienceSvc.AskResearch(h.enrichContext(c), id, req.Question)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(fiber.Map{"answer": answer})
}

func (h *Handler) AskAudienceEveryone(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req studyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	group, calls, err := h.audienceSvc.AskEveryone(h.enrichContext(c), id, req.Question)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"group_id": group, "calls": calls})
}

func (h *Handler) FollowUpAudiencePersona(c *fiber.Ctx) error {
	studyID, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	personaID, err := audienceID(c, "persona")
	if err != nil {
		return err
	}
	var req studyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.audienceSvc.FollowUp(h.enrichContext(c), studyID, personaID, req.Question)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) GetAudienceThread(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	out, err := h.audienceSvc.Thread(h.enrichContext(c), id)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(fiber.Map{"messages": out})
}

func (h *Handler) SendAudienceMessage(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.audienceSvc.Chat(h.enrichContext(c), id, req.Message)
	if err != nil {
		return audienceFailure(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) ClearAudienceThread(c *fiber.Ctx) error {
	id, err := audienceID(c, "id")
	if err != nil {
		return err
	}
	if err := h.audienceSvc.ClearThread(h.enrichContext(c), id); err != nil {
		return audienceFailure(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
