package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
)

// registerRepoDocsRoutes exposes the per-repository reference-doc task: "go
// write the missing coding-standards / test-standards / architecture /
// local-run doc".
//
// Two ways to ask for the same thing: the bundle route opens ONE task for
// several docs, so they land in one pull request, and the per-kind route
// (registered last, and only reachable for a :kind that is not "task") still
// opens one task for one doc.
func (h *Handler) registerRepoDocsRoutes(app fiber.Router) {
	if h.repoDocsSvc == nil {
		return
	}
	app.Post("/v1/repositories/:id/docs/setup-task", h.CreateRepoDocsBundleTask)
	app.Get("/v1/repositories/:id/docs/task", h.GetRepoDocsTask)
	app.Post("/v1/repositories/:id/docs/task/merge", h.MergeRepoDocsTask)
	app.Post("/v1/repositories/:id/docs/:kind/setup-task", h.CreateRepoDocTask)
}

// CreateRepoDocTask — POST /v1/repositories/:id/docs/:kind/setup-task?sub_project_path=&path=
func (h *Handler) CreateRepoDocTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	task, err := h.repoDocsSvc.CreateDocTask(h.enrichContext(c), id, c.Query("sub_project_path"), c.Params("kind"), c.Query("path"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}

// CreateRepoDocsBundleTask — POST /v1/repositories/:id/docs/setup-task
// Body: {"items":[{"kind":"architecture","component_id":"","path":""}]}
// Opens one board task for every requested doc, so all of them arrive in a
// single pull request.
func (h *Handler) CreateRepoDocsBundleTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Items []repodocs.DocItem `json:"items"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	task, err := h.repoDocsSvc.CreateDocsBundleTask(h.enrichContext(c), id, req.Items)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"task_id": task.ID.String(), "task": task})
}

// GetRepoDocsTask — GET /v1/repositories/:id/docs/task
// The outstanding doc bundle's task and pull request; every field empty when
// there is none.
func (h *Handler) GetRepoDocsTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	status, err := h.repoDocsSvc.DocsTask(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(status)
}

// MergeRepoDocsTask — POST /v1/repositories/:id/docs/task/merge
// Merges the outstanding doc bundle's pull request into the default branch.
func (h *Handler) MergeRepoDocsTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	result, err := h.repoDocsSvc.MergeDocsTask(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(result)
}
