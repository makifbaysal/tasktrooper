package http

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReleaseWaker is application/release.Service's WakeTask, narrowed so this
// package need not import application/release. Declared separately from
// ReleaseService (handler_release.go): the before-deploy confirm route below
// must keep working on a build with no release service wired up at all.
type ReleaseWaker interface {
	WakeTask(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error
}

func (h *Handler) registerRepositoryRoutes(app fiber.Router) {
	if h.repositorySvc == nil {
		return
	}
	app.Get("/v1/repositories", h.ListRepositories)
	app.Post("/v1/repositories/open", h.OpenRepository)
	app.Post("/v1/repositories/import", h.ImportGitHubRepository)
	app.Post("/v1/repositories", h.CreateRepository)
	app.Post("/v1/repositories/:id/restore", h.RestoreRepositoryWorkingCopy)
	app.Get("/v1/repositories/:id", h.GetRepository)
	app.Get("/v1/repositories/:id/directories", h.ListRepositoryDirectories)
	app.Patch("/v1/repositories/:id", h.UpdateRepository)
	app.Delete("/v1/repositories/:id", h.DeleteRepository)
	app.Put("/v1/repositories/:id/projects", h.SetRepositoryProjects)
	app.Get("/v1/repositories/:id/index/status", h.RepositoryIndexStatus)
	app.Post("/v1/repositories/:id/index", h.ReindexRepository)
	app.Delete("/v1/repositories/:id/index", h.StopRepositoryIndex)
	app.Post("/v1/repositories/:id/webhook", h.SetupRepositoryWebhook)
	// Public (signature-authenticated) — see isPublicPath and githubWebhookPath.
	app.Post(githubWebhookPath, h.GitHubWebhook)
	app.Post("/v1/repositories/:id/index/search", h.SearchRepositoryIndex)
	app.Get("/v1/repositories/:id/tasks", h.ListRepositoryTasks)
	app.Post("/v1/repositories/:id/tasks", h.CreateRepositoryTask)
	app.Patch("/v1/repositories/:id/tasks/:taskId", h.UpdateRepositoryTask)
	app.Delete("/v1/repositories/:id/tasks/:taskId", h.DeleteRepositoryTask)
	app.Post("/v1/repositories/:id/tasks/:taskId/before-deploy/confirm", h.ConfirmTaskBeforeDeploy)
	app.Get("/v1/repositories/:id/tasks/:taskId/comments", h.ListTaskComments)
	app.Post("/v1/repositories/:id/tasks/:taskId/comments", h.CreateTaskComment)
	app.Get("/v1/repositories/:id/tasks/:taskId/documents", h.ListTaskDocuments)
	app.Post("/v1/repositories/:id/tasks/:taskId/documents", h.CreateTaskDocument)
	app.Patch("/v1/repositories/:id/tasks/:taskId/documents/:docId", h.UpdateTaskDocument)
	app.Delete("/v1/repositories/:id/tasks/:taskId/documents/:docId", h.DeleteTaskDocument)
	app.Get("/v1/repositories/:id/tasks/:taskId/acceptance-criteria", h.ListAcceptanceCriteria)
	app.Put("/v1/repositories/:id/tasks/:taskId/acceptance-criteria", h.ReplaceAcceptanceCriteria)
	app.Patch("/v1/repositories/:id/tasks/:taskId/acceptance-criteria/:criterionId", h.UpdateAcceptanceCriterion)
	app.Get("/v1/repositories/:id/tasks/:taskId/test-cases", h.ListTestCases)
	app.Put("/v1/repositories/:id/tasks/:taskId/test-cases", h.ReplaceTestCases)
	app.Patch("/v1/repositories/:id/tasks/:taskId/test-cases/:testCaseId", h.UpdateTestCase)
	app.Delete("/v1/repositories/:id/tasks/:taskId/test-cases/:testCaseId", h.DeleteTestCase)
	app.Get("/v1/repositories/:id/tasks/:taskId/runs", h.ListTaskAgentRuns)
	app.Post("/v1/repositories/:id/tasks/:taskId/runs/:runId/cancel", h.CancelTaskAgentRun)
	app.Post("/v1/repositories/:id/tasks/:taskId/runs/:runId/rerun", h.RerunTaskAgentRun)
	app.Post("/v1/repositories/:id/tasks/:taskId/chat", h.OpenTaskChat)
	app.Get("/v1/repositories/:id/local-preview", h.GetLocalPreview)
	app.Post("/v1/repositories/:id/tasks/:taskId/local-preview/start", h.StartLocalPreview)
	app.Post("/v1/repositories/:id/local-preview/stop", h.StopLocalPreview)
	app.Get("/v1/repositories/:id/tasks/:taskId/events", h.ListTaskEvents)
	app.Get("/v1/repositories/:id/tasks/:taskId/pipelines", h.ListTaskPipelines)
	app.Get("/v1/repositories/:id/tasks/:taskId/pipelines/:pipelineId", h.GetTaskPipeline)
	app.Post("/v1/repositories/:id/tasks/:taskId/pipelines", h.TriggerTaskPipeline)
	app.Post("/v1/repositories/:id/pipeline/setup-task", h.CreateWorkflowSetupTask)
}

// CreateWorkflowSetupTask — POST /v1/repositories/:id/pipeline/setup-task
// Opens a board task (assigned by repo kind) to author the repo's CI/CD
// GitHub Actions workflows, for repos that have none yet.
func (h *Handler) CreateWorkflowSetupTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	task, err := h.repositorySvc.CreateWorkflowSetupTask(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}

func (h *Handler) registerInitiativeRoutes(app fiber.Router) {
	if h.initiativeSvc == nil {
		return
	}
	app.Get("/v1/projects", h.ListInitiativeProjects)
	app.Post("/v1/projects", h.CreateInitiativeProject)
	app.Get("/v1/projects/:projectId", h.GetInitiativeProject)
	app.Patch("/v1/projects/:projectId", h.UpdateInitiativeProject)
	app.Delete("/v1/projects/:projectId", h.DeleteInitiativeProject)
	app.Get("/v1/tasks/lookup", h.LookupTask)
}

func (h *Handler) ListRepositories(c *fiber.Ctx) error {
	repos, err := h.repositorySvc.List(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"repositories": repos, "count": len(repos)})
}

func (h *Handler) OpenRepository(c *fiber.Ctx) error {
	var req domain.OpenRepositoryRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.RootPath == "" {
		return badRequest(c, "root_path is required")
	}
	repo, err := h.repositorySvc.Open(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(repo)
}

// ImportGitHubRepository — POST /v1/repositories/import
func (h *Handler) ImportGitHubRepository(c *fiber.Ctx) error {
	var req domain.ImportGitHubRepositoryRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repo, err := h.repositorySvc.ImportFromGitHub(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(repo)
}

func (h *Handler) CreateRepository(c *fiber.Ctx) error {
	var req domain.CreateRepositoryRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" {
		return badRequest(c, "name is required")
	}
	repo, err := h.repositorySvc.Create(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(repo)
}

func (h *Handler) GetRepository(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	repo, err := h.repositorySvc.Get(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(repo)
}

// ListRepositoryDirectories — GET /v1/repositories/:id/directories?path=
//
// Lists the immediate subdirectories under path (repo-relative, root when
// omitted) inside the repository's working copy. Backs the folder picker used
// to manually add a monorepo sub-project when auto-detection missed it or
// found nothing — the human browses real directories instead of typing a path
// that may or may not exist.
func (h *Handler) ListRepositoryDirectories(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	rel, names, err := h.repositorySvc.ListDirectories(h.enrichContext(c), id, c.Query("path"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	entries := make([]fiber.Map, 0, len(names))
	for _, name := range names {
		childPath := name
		if rel != "" {
			childPath = rel + "/" + name
		}
		entries = append(entries, fiber.Map{"name": name, "path": childPath})
	}
	var parent *string
	if rel != "" {
		p := ""
		if idx := strings.LastIndex(rel, "/"); idx >= 0 {
			p = rel[:idx]
		}
		parent = &p
	}
	return c.JSON(fiber.Map{"path": rel, "parent": parent, "entries": entries})
}

// RestoreRepositoryWorkingCopy — POST /v1/repositories/:id/restore
//
// Clones a registered repository from its recorded remote into this runtime's
// own workspace and re-points root_path at it. Returns 202 with the repository:
// the clone runs in the background and is reported by `git_restore` on any
// later read, because a large repository takes minutes and no HTTP handler
// should hold a connection (or a browser tab) open for that.
//
// A refusal is a 400 carrying the reason — the folder is already there, it
// holds something that is not a repository, the path cannot be read, or no
// remote is recorded. Nothing on disk is ever removed to make room.
func (h *Handler) RestoreRepositoryWorkingCopy(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	repo, err := h.repositorySvc.RestoreWorkingCopy(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusAccepted).JSON(repo)
}

// UpdateRepository — PATCH /v1/repositories/:id
//
// kind, sub_repo_kinds, sub_projects and mobile_platform are refused: the
// project model now projects them from the component scan, so a client
// setting them directly would be overwritten by the next scan anyway.
func (h *Handler) UpdateRepository(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.UpdateRepositoryRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Kind != nil || req.SubRepoKinds != nil || req.SubProjects != nil || req.MobilePlatform != nil {
		return badRequest(c, "set roles on components instead")
	}
	repo, err := h.repositorySvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(repo)
}

func (h *Handler) DeleteRepository(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.repositorySvc.Delete(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) SetRepositoryProjects(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.SetRepositoryProjectsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repo, err := h.repositorySvc.SetProjects(h.enrichContext(c), id, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(repo)
}

func (h *Handler) RepositoryIndexStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	idx, err := h.repositorySvc.IndexStatus(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(idx)
}

func (h *Handler) SearchRepositoryIndex(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.IndexSearchRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	results, err := h.repositorySvc.SearchIndex(h.enrichContext(c), id, req.Query, req.TopK)
	if err != nil {
		// badRequestErr, not badRequest: this is the shortest synchronous path
		// from a person to the Mac. Answering a query means EMBEDDING it, and
		// the only embedder is LM Studio on the acting member's laptop — so
		// "your Mac is not connected" is one of the ordinary outcomes of typing
		// in the search box, and it needs the 409 the clients have a screen
		// for rather than a 400 that reads as "bad query".
		return badRequestErr(c, err)
	}
	return c.JSON(fiber.Map{"results": results, "count": len(results)})
}

func (h *Handler) ReindexRepository(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.repositorySvc.Reindex(h.enrichContext(c), id); err != nil {
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusAccepted)
}

// StopRepositoryIndex cancels a running index pass. The partial index stays:
// every file already indexed keeps its rows and its hash, so a later run
// resumes instead of restarting.
func (h *Handler) StopRepositoryIndex(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	stopped, err := h.repositorySvc.StopIndex(h.enrichContext(c), id)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"stopped": stopped})
}

func (h *Handler) ListRepositoryTasks(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	tasks, err := h.repositorySvc.ListTasks(h.enrichContext(c), id)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"tasks": tasks, "count": len(tasks)})
}

func (h *Handler) CreateRepositoryTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.CreateBoardTaskRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Title == "" {
		return badRequest(c, "title is required")
	}
	task, err := h.repositorySvc.CreateTask(h.enrichContext(c), id, req)
	if err != nil {
		return badRequestErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}

func (h *Handler) UpdateRepositoryTask(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	var req domain.UpdateBoardTaskRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	// Everything arriving over the HTTP API is a human acting on the board.
	// Actor is not parsed from the body, so a client cannot claim to be an
	// agent and dodge the review gate.
	req.Actor = domain.TaskActorHuman
	task, err := h.repositorySvc.UpdateTask(h.enrichContext(c), repositoryID, taskID, req)
	if err != nil {
		return badRequestErr(c, err)
	}
	return c.JSON(task)
}

func (h *Handler) DeleteRepositoryTask(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	if err := h.repositorySvc.DeleteTask(h.enrichContext(c), repositoryID, taskID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ConfirmTaskBeforeDeploy — POST /v1/repositories/:id/tasks/:taskId/before-deploy/confirm
//
// No body: this is a single-user app, and the confirmation IS the human
// having clicked the button after the UI asked "did you do these steps?".
// When the task is sitting in done, this also wakes the release engineer on
// it — the same nudge a newly-confirmed delivery profile gives — so a late
// confirmation does not have to wait for the next poll.
func (h *Handler) ConfirmTaskBeforeDeploy(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	ctx := h.enrichContext(c)
	task, err := h.repositorySvc.ConfirmBeforeDeploy(ctx, repositoryID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrBoardTaskNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: "not_found"},
			})
		}
		return internalError(c, err)
	}
	if h.releaseWaker != nil && task.Column == domain.TaskColumnDone {
		if werr := h.releaseWaker.WakeTask(ctx, repositoryID, task); werr != nil {
			log.Warn().Err(werr).Str("task_id", taskID.String()).
				Msg("wake release engineer after before-deploy confirm failed")
		}
	}
	return c.JSON(task)
}

func (h *Handler) ListTaskDocuments(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	docs, err := h.repositorySvc.ListDocuments(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"documents": docs, "count": len(docs)})
}

func (h *Handler) CreateTaskDocument(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.CreateTaskDocumentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Title == "" {
		return badRequest(c, "title is required")
	}
	doc, err := h.repositorySvc.AddDocument(h.enrichContext(c), repositoryID, taskID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(doc)
}

func (h *Handler) UpdateTaskDocument(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	docID, err := uuid.Parse(c.Params("docId"))
	if err != nil {
		return badRequest(c, "invalid document id")
	}
	var req domain.UpdateTaskDocumentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	doc, err := h.repositorySvc.UpdateDocument(h.enrichContext(c), repositoryID, taskID, docID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(doc)
}

func (h *Handler) DeleteTaskDocument(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	docID, err := uuid.Parse(c.Params("docId"))
	if err != nil {
		return badRequest(c, "invalid document id")
	}
	if err := h.repositorySvc.DeleteDocument(h.enrichContext(c), repositoryID, taskID, docID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListAcceptanceCriteria backs the task drawer. The task objects the board
// lists do not carry criteria, so without this route the drawer had no way to
// read them and always rendered an empty list.
func (h *Handler) ListAcceptanceCriteria(c *fiber.Ctx) error {
	_, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	items, err := h.repositorySvc.ListTaskCriteria(h.enrichContext(c), taskID)
	if err != nil {
		return internalError(c, err)
	}
	if items == nil {
		items = []domain.AcceptanceCriterion{}
	}
	return c.JSON(fiber.Map{"items": items})
}

func (h *Handler) ReplaceAcceptanceCriteria(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		Items []domain.AcceptanceCriterionInput `json:"items"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	items, err := h.repositorySvc.ReplaceAcceptanceCriteria(h.enrichContext(c), repositoryID, taskID, req.Items)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"items": items})
}

func (h *Handler) UpdateAcceptanceCriterion(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	criterionID, err := uuid.Parse(c.Params("criterionId"))
	if err != nil {
		return badRequest(c, "invalid criterion id")
	}
	// Two different edits share this route: ticking the box, and dropping the
	// criterion with a reason. `canceled` is a pointer so its absence means
	// "not part of this request" — a plain tick must not un-cancel by omission.
	var req struct {
		Completed    bool   `json:"completed"`
		Canceled     *bool  `json:"canceled"`
		CancelReason string `json:"cancel_reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Canceled != nil {
		if _, err := h.repositorySvc.GetTask(h.enrichContext(c), repositoryID, taskID); err != nil {
			return badRequest(c, err.Error())
		}
		item, err := h.repositorySvc.SetTaskCriterionCanceled(h.enrichContext(c), criterionID, *req.Canceled, req.CancelReason, "user", "")
		if err != nil {
			return badRequest(c, err.Error())
		}
		return c.JSON(item)
	}
	item, err := h.repositorySvc.UpdateCriterionCompleted(h.enrichContext(c), repositoryID, taskID, criterionID, req.Completed)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(item)
}

// ListTestCases backs the test-case panel in the task drawer, the same way
// ListAcceptanceCriteria backs the criteria one.
func (h *Handler) ListTestCases(c *fiber.Ctx) error {
	_, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	items, err := h.repositorySvc.ListTestCases(h.enrichContext(c), taskID)
	if err != nil {
		return internalError(c, err)
	}
	if items == nil {
		items = []domain.TaskTestCase{}
	}
	return c.JSON(fiber.Map{"items": items, "summary": domain.SummarizeTestCases(items)})
}

func (h *Handler) ReplaceTestCases(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		Items []domain.TaskTestCaseInput `json:"items"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	items, err := h.repositorySvc.ReplaceTestCases(h.enrichContext(c), repositoryID, taskID, req.Items)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"items": items, "summary": domain.SummarizeTestCases(items)})
}

func (h *Handler) UpdateTestCase(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	testCaseID, err := uuid.Parse(c.Params("testCaseId"))
	if err != nil {
		return badRequest(c, "invalid test case id")
	}
	var req domain.TaskTestCaseInput
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	item, err := h.repositorySvc.UpdateTestCase(h.enrichContext(c), repositoryID, taskID, testCaseID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(item)
}

func (h *Handler) DeleteTestCase(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	testCaseID, err := uuid.Parse(c.Params("testCaseId"))
	if err != nil {
		return badRequest(c, "invalid test case id")
	}
	if err := h.repositorySvc.DeleteTestCase(h.enrichContext(c), repositoryID, taskID, testCaseID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListTaskPipelines(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	pipelines, err := h.repositorySvc.ListTaskPipelines(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"pipelines": pipelines})
}

func (h *Handler) GetTaskPipeline(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	pipelineID, err := uuid.Parse(c.Params("pipelineId"))
	if err != nil {
		return badRequest(c, "invalid pipeline id")
	}
	pipeline, err := h.repositorySvc.GetTaskPipeline(h.enrichContext(c), repositoryID, taskID, pipelineID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(pipeline)
}

func (h *Handler) TriggerTaskPipeline(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	pipeline, err := h.repositorySvc.TriggerTaskPipeline(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrPipelineActive) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "conflict"}})
		}
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(pipeline)
}

func (h *Handler) LookupTask(c *fiber.Ctx) error {
	key := c.Query("key")
	if key == "" {
		return badRequest(c, "key is required")
	}
	task, err := h.repositorySvc.LookupTaskByKey(h.enrichContext(c), key)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(task)
}

func parseRepositoryTaskParams(c *fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return repositoryID, taskID, nil
}
