package http

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/githubauth"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// GitHubAppService is *githubauth.Service: the GitHub App connection made
// through the device flow.
type GitHubAppService interface {
	AppStatus(ctx context.Context) (githubauth.Status, bool, error)
	StartDeviceFlow(ctx context.Context) (domain.GitHubDeviceFlow, error)
	PollDeviceFlow(ctx context.Context, id string) (domain.GitHubDeviceFlow, error)
}

// GitHub bağlantısı doğrudan yapıştırılan bir personal access token ile
// kurulur (SetGitHubToken); kullanıcı token'ı GitHub'da oluşturup buraya
// yapıştırır. Token doğrulanır (githubapi.User) ve şifreli olarak
// app_settings'te saklanır; gh CLI kullanılmaz.

type githubStatusResponse struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Detail    string `json:"detail,omitempty"`
	// MissingScopes are the scopes a classic token lacks among the ones
	// TaskTrooper needs (workflow, so an agent may add or edit CI files).
	MissingScopes []string `json:"missing_scopes,omitempty"`
	// FineGrained: the token's permissions cannot be read back, so the UI
	// can only remind what it needs.
	FineGrained bool `json:"fine_grained,omitempty"`
	// Mode is "app" for a device-flow connection, "token" for a pasted one.
	Mode domain.GitHubConnectionMode `json:"mode,omitempty"`
	// AppAvailable: a GitHub App is configured, so "Connect with GitHub" works.
	AppAvailable bool   `json:"app_available"`
	NeedsInstall bool   `json:"needs_install,omitempty"`
	InstallURL   string `json:"install_url,omitempty"`
	Expired      bool   `json:"expired,omitempty"`
}

// requiredGitHubScopes are checked on a classic token; repo alone cannot
// push a change under .github/workflows.
var requiredGitHubScopes = []string{"repo", "workflow"}

func withScopes(ctx context.Context, token string, out githubStatusResponse) githubStatusResponse {
	scopes, classic, err := githubapi.TokenScopes(ctx, token)
	if err != nil {
		return out
	}
	if !classic {
		out.FineGrained = true
		return out
	}
	out.MissingScopes = githubapi.MissingScopes(scopes, requiredGitHubScopes)
	return out
}

// GitHubStatus — GET /v1/settings/github
func (h *Handler) GitHubStatus(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.JSON(githubStatusResponse{Connected: false, Detail: "token store not configured"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()

	base := githubStatusResponse{}
	if h.githubApp != nil {
		st, isApp, err := h.githubApp.AppStatus(ctx)
		if err != nil {
			return internalError(c, err)
		}
		base.AppAvailable, base.InstallURL = st.AppAvailable, st.InstallURL
		if isApp {
			base.Mode, base.Login, base.NeedsInstall, base.Expired = st.Mode, st.Login, st.NeedsInstall, st.Expired
			base.Connected = !st.Expired
			if st.Expired {
				base.Detail = domain.ErrGitHubConnectionExpired.Error()
			}
			return c.JSON(base)
		}
	}

	token, err := h.githubTokens.GitHubToken(ctx)
	if err != nil {
		return internalError(c, err)
	}
	if token == "" {
		return c.JSON(base)
	}
	login, err := githubapi.User(ctx, token)
	if err != nil {
		base.Detail = "token is invalid or GitHub is unreachable: " + err.Error()
		return c.JSON(base)
	}
	base.Connected, base.Login, base.Mode = true, login, domain.GitHubModeToken
	return c.JSON(withScopes(ctx, token, base))
}

// SetGitHubToken — PUT /v1/settings/github {"token":"..."}
func (h *Handler) SetGitHubToken(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return badRequest(c, "token is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()

	login, err := githubapi.User(ctx, token)
	if err != nil {
		return badRequest(c, "token could not be verified: "+err.Error())
	}
	if err := h.githubTokens.SetGitHubToken(ctx, token); err != nil {
		return internalError(c, err)
	}
	return c.JSON(withScopes(ctx, token, githubStatusResponse{Connected: true, Login: login, Mode: domain.GitHubModeToken}))
}

// GitHubOwners — GET /v1/settings/github/owners
// Token sahibi + üyesi olduğu org'lar (repo import/create hedefleri).
func (h *Handler) GitHubOwners(c *fiber.Ctx) error {
	token, ok, err := h.requireGitHubToken(c)
	if err != nil || !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	owners, aerr := githubapi.ListOwners(ctx, token)
	if aerr != nil {
		return badRequest(c, "failed to list GitHub owners: "+aerr.Error())
	}
	return c.JSON(fiber.Map{"owners": owners})
}

// GitHubOwnerRepos — GET /v1/settings/github/repos?owner=<login>
func (h *Handler) GitHubOwnerRepos(c *fiber.Ctx) error {
	token, ok, err := h.requireGitHubToken(c)
	if err != nil || !ok {
		return err
	}
	owner := strings.TrimSpace(c.Query("owner"))
	if owner == "" {
		return badRequest(c, "owner is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	repos, aerr := githubapi.ListOwnerRepos(ctx, token, owner)
	if aerr != nil {
		return badRequest(c, "failed to list GitHub repositories: "+aerr.Error())
	}
	return c.JSON(fiber.Map{"repos": repos})
}

// requireGitHubToken: token yoksa 400 döner (ok=false); hata zaten yazılmıştır.
func (h *Handler) requireGitHubToken(c *fiber.Ctx) (string, bool, error) {
	if h.githubTokens == nil {
		return "", false, c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	token, err := h.githubTokens.GitHubToken(c.UserContext())
	if err != nil {
		return "", false, internalError(c, err)
	}
	if token == "" {
		return "", false, badRequest(c, "GitHub is not connected — connect it from Settings")
	}
	return token, true, nil
}

// DeleteGitHubToken — DELETE /v1/settings/github
func (h *Handler) DeleteGitHubToken(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	if err := h.githubTokens.DeleteGitHubToken(c.UserContext()); err != nil {
		return internalError(c, err)
	}
	return c.JSON(githubStatusResponse{Connected: false})
}

func (h *Handler) requireGitHubApp(c *fiber.Ctx) bool {
	if h.githubApp != nil {
		return true
	}
	_ = c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "GitHub App connection not configured", Type: "service_unavailable"},
	})
	return false
}

// StartGitHubDeviceFlow — POST /v1/settings/github/device
// Answers the code the user types at verification_uri; the device code
// itself stays on the server.
func (h *Handler) StartGitHubDeviceFlow(c *fiber.Ctx) error {
	if !h.requireGitHubApp(c) {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	flow, err := h.githubApp.StartDeviceFlow(ctx)
	if errors.Is(err, domain.ErrGitHubAppNotConfigured) {
		return badRequest(c, err.Error())
	}
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(errorResponse{Error: errorDetail{Message: err.Error(), Type: "github_unavailable"}})
	}
	return c.JSON(flow)
}

// PollGitHubDeviceFlow — GET /v1/settings/github/device/:id
// Safe to call as often as the UI likes: the server asks GitHub no faster
// than the interval GitHub set.
func (h *Handler) PollGitHubDeviceFlow(c *fiber.Ctx) error {
	if !h.requireGitHubApp(c) {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	flow, err := h.githubApp.PollDeviceFlow(ctx, c.Params("id"))
	if errors.Is(err, githubauth.ErrDeviceFlowNotFound) {
		return notFound(c, err.Error())
	}
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(flow)
}
