package http

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerDataSourceRoutes mounts observability data sources, their kinds,
// and the signal/recipe catalog. See .ai/api-spec.md for the contract.
func (h *Handler) registerDataSourceRoutes(app fiber.Router) {
	if h.dataSourceSvc == nil {
		return
	}
	app.Get("/v1/data-source-kinds", h.ListDataSourceKinds)
	app.Get("/v1/data-sources", h.ListDataSources)
	app.Post("/v1/data-sources", h.CreateDataSource)
	app.Get("/v1/data-sources/:id", h.GetDataSource)
	app.Patch("/v1/data-sources/:id", h.UpdateDataSource)
	app.Delete("/v1/data-sources/:id", h.DeleteDataSource)
	app.Post("/v1/data-sources/:id/verify", h.VerifyDataSource)
	app.Get("/v1/data-sources/:id/discover", h.DiscoverDataSource)
	app.Post("/v1/data-sources/:id/query", h.QueryDataSource)
	app.Get("/v1/signals", h.ListSignals)
	app.Get("/v1/recipes", h.ListRecipes)
}

// dataSourceErr writes the flat {"error"} shape, plus "fields" for a request
// the kind's schema refuses and "code" where the UI must tell cases apart.
func dataSourceErr(c *fiber.Ctx, err error) error {
	var fe datasource.FieldErrors
	switch {
	case errors.As(err, &fe):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "fields": fe})
	case errors.Is(err, port.ErrNotFound):
		return cErr(c, fiber.StatusNotFound, err)
	case errors.Is(err, datasource.ErrUnsupported):
		return cErrCode(c, fiber.StatusUnprocessableEntity, err, "unsupported")
	case errors.Is(err, port.ErrDataSourceQuery):
		return cErrCode(c, fiber.StatusBadRequest, err, "bad_query")
	case errors.Is(err, datasource.ErrUpstream):
		return cErrCode(c, fiber.StatusBadGateway, err, "upstream")
	case errors.Is(err, datasource.ErrInvalidInput):
		return cErr(c, fiber.StatusBadRequest, err)
	default:
		return cErr(c, fiber.StatusInternalServerError, err)
	}
}

// ListDataSourceKinds — GET /v1/data-source-kinds
func (h *Handler) ListDataSourceKinds(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"kinds": h.dataSourceSvc.Kinds()})
}

// ListDataSources — GET /v1/data-sources
func (h *Handler) ListDataSources(c *fiber.Ctx) error {
	list, err := h.dataSourceSvc.List(h.enrichContext(c))
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(fiber.Map{"data_sources": list})
}

// CreateDataSource — POST /v1/data-sources {kind, name, config, secrets}
// 201 even when the source cannot be reached: status says how the verify
// went. 400 with "fields" when the request does not fit the kind's schema.
func (h *Handler) CreateDataSource(c *fiber.Ctx) error {
	var req domain.SaveDataSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	ds, err := h.dataSourceSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(ds)
}

// GetDataSource — GET /v1/data-sources/:id
func (h *Handler) GetDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	ds, err := h.dataSourceSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(ds)
}

// UpdateDataSource — PATCH /v1/data-sources/:id {name?, config?, secrets?}
// A key given replaces its value, an empty string removes it; the result is
// verified again.
func (h *Handler) UpdateDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	var req domain.PatchDataSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	ds, err := h.dataSourceSvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(ds)
}

// DeleteDataSource — DELETE /v1/data-sources/:id
func (h *Handler) DeleteDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	if err := h.dataSourceSvc.Delete(h.enrichContext(c), id); err != nil {
		return dataSourceErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// VerifyDataSource — POST /v1/data-sources/:id/verify
// 200 either way; a failure is reported through status/status_detail.
func (h *Handler) VerifyDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	ds, err := h.dataSourceSvc.Verify(h.enrichContext(c), id)
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(ds)
}

// DiscoverDataSource — GET /v1/data-sources/:id/discover?match=&limit=
func (h *Handler) DiscoverDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 0 {
			return dataSourceErr(c, datasource.FieldErrors{"limit": datasource.FieldInvalid})
		}
	}
	d, err := h.dataSourceSvc.Discover(h.enrichContext(c), id, c.Query("match"), limit)
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(d)
}

type dataSourceQueryRequest struct {
	Query       string  `json:"query"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	StepSeconds float64 `json:"step_seconds"`
}

// QueryDataSource — POST /v1/data-sources/:id/query {query, from, to, step_seconds}
// from/to are RFC3339; step_seconds 0 picks one. 422 code "unsupported" for a
// kind that cannot query metrics, 400 code "bad_query" for a query the source
// refused.
func (h *Handler) QueryDataSource(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid data source id")
	}
	var req dataSourceQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	fe := datasource.FieldErrors{}
	from, ferr := time.Parse(time.RFC3339, req.From)
	if ferr != nil {
		fe["from"] = datasource.FieldInvalid
	}
	to, terr := time.Parse(time.RFC3339, req.To)
	if terr != nil {
		fe["to"] = datasource.FieldInvalid
	}
	if req.StepSeconds < 0 {
		fe["step_seconds"] = datasource.FieldInvalid
	}
	if len(fe) > 0 {
		return dataSourceErr(c, fe)
	}
	res, err := h.dataSourceSvc.Query(h.enrichContext(c), id, domain.SeriesQuery{
		Query: req.Query,
		From:  from,
		To:    to,
		Step:  time.Duration(req.StepSeconds * float64(time.Second)),
	})
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(res)
}

// ListSignals — GET /v1/signals
func (h *Handler) ListSignals(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"signals": h.dataSourceSvc.Signals()})
}

// ListRecipes — GET /v1/recipes
func (h *Handler) ListRecipes(c *fiber.Ctx) error {
	recipes, err := h.dataSourceSvc.Recipes(h.enrichContext(c))
	if err != nil {
		return dataSourceErr(c, err)
	}
	return c.JSON(fiber.Map{"recipes": recipes})
}
