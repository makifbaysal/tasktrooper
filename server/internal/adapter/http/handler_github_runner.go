package http

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// GitHubRunnerService is *ghrunner.Manager: this computer as a self-hosted
// GitHub Actions runner.
type GitHubRunnerService interface {
	Status(ctx context.Context) (domain.GitHubRunnerStatus, error)
	Configure(ctx context.Context, cfg domain.GitHubRunnerConfig) (domain.GitHubRunnerStatus, error)
}

// GitHubRunnerStatus — GET /v1/settings/github/runner
func (h *Handler) GitHubRunnerStatus(c *fiber.Ctx) error {
	if h.githubRunner == nil {
		return c.JSON(domain.GitHubRunnerStatus{Labels: []string{}, Repos: []domain.GitHubRunnerRepoStatus{}, Candidates: []string{}})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()
	st, err := h.githubRunner.Status(ctx)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(st)
}

// SetGitHubRunner — PUT /v1/settings/github/runner {"enabled":true,"repos":["owner/name"]}
// A refused repository answers 400 with type "github_runner_<code>" and the
// repository in the message.
func (h *Handler) SetGitHubRunner(c *fiber.Ctx) error {
	if h.githubRunner == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "this server cannot run a GitHub runner", Type: "github_runner_" + domain.GitHubRunnerErrUnsupported},
		})
	}
	var req domain.GitHubRunnerConfig
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	st, err := h.githubRunner.Configure(ctx, req)
	var repoErr *domain.GitHubRunnerRepoError
	if errors.As(err, &repoErr) {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: repoErr.Error(), Type: "github_runner_" + repoErr.Code},
		})
	}
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(st)
}
