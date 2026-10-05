package http

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
)

// registerEmbeddingMapRoutes exposes the projection the web UI feeds to UMAP.
// Same router group — and therefore the same auth middleware — as every
// other /v1 data route.
func (h *Handler) registerEmbeddingMapRoutes(app fiber.Router) {
	if h.embedMapSvc == nil {
		return
	}
	app.Get("/v1/embedding-map/sources", h.EmbeddingMapSources)
	app.Get("/v1/embedding-map", h.EmbeddingMap)
	app.Post("/v1/embedding-map/locate", h.EmbeddingMapLocate)
}

func (h *Handler) EmbeddingMapSources(c *fiber.Ctx) error {
	sources, err := h.embedMapSvc.Sources(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(sources)
}

func (h *Handler) EmbeddingMap(c *fiber.Ctx) error {
	q := embedmap.Query{
		Source: c.Query("source"),
		Limit:  queryIntOr(c, "limit", embedmap.DefaultLimit),
		Dims:   queryIntOr(c, "dims", embedmap.DefaultDims),
	}
	switch q.Source {
	case embedmap.SourceFiles:
		// repository_id is meaningless for uploaded documents; ignore it.
	case embedmap.SourceCode:
		id, err := uuid.Parse(c.Query("repository_id"))
		if err != nil {
			return badRequest(c, embedmap.ErrRepositoryRequired.Error())
		}
		q.RepositoryID = id
	default:
		return badRequest(c, embedmap.ErrInvalidSource.Error())
	}

	result, err := h.embedMapSvc.Build(h.enrichContext(c), q)
	if err != nil {
		if errors.Is(err, embedmap.ErrInvalidSource) || errors.Is(err, embedmap.ErrRepositoryRequired) {
			return badRequest(c, err.Error())
		}
		return internalError(c, err)
	}
	return c.JSON(result)
}

type embeddingMapLocateRequest struct {
	RepositoryID string   `json:"repository_id"`
	AnchorIDs    []string `json:"anchor_ids"`
	ChunkIDs     []string `json:"chunk_ids"`
}

func (h *Handler) EmbeddingMapLocate(c *fiber.Ctx) error {
	var req embeddingMapLocateRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repoID, err := uuid.Parse(req.RepositoryID)
	if err != nil {
		return badRequest(c, embedmap.ErrLocateRepositoryRequired.Error())
	}
	result, err := h.embedMapSvc.Locate(h.enrichContext(c), embedmap.LocateQuery{
		RepositoryID: repoID,
		AnchorIDs:    req.AnchorIDs,
		ChunkIDs:     req.ChunkIDs,
	})
	if err != nil {
		if errors.Is(err, embedmap.ErrLocateAnchorsRequired) || errors.Is(err, embedmap.ErrLocateChunksRequired) ||
			errors.Is(err, embedmap.ErrLocateTooManyAnchors) || errors.Is(err, embedmap.ErrLocateTooManyChunks) ||
			errors.Is(err, embedmap.ErrLocateRepositoryRequired) {
			return badRequest(c, err.Error())
		}
		return internalError(c, err)
	}
	return c.JSON(result)
}

// queryIntOr reads an integer query param, falling back to def when it is
// absent or unparseable. Range checking belongs to the service, which clamps
// rather than rejects.
func queryIntOr(c *fiber.Ctx, name string, def int) int {
	raw := c.Query(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
