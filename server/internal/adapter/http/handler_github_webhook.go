package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// githubWebhookPath is the delivery endpoint, named once because two separate
// decisions key off it and a disagreement between them is a security bug rather
// than a 404:
//
//   - the route itself (handler_repository.go),
//   - isPublicPath (handler_ui.go), which exempts it from the API key because
//     GitHub holds none of our credentials.
//
// A literal that drifted in one file would silently produce either a public
// path that is not the webhook or a webhook GitHub can never reach.
const githubWebhookPath = "/v1/github/webhook"

// githubPushPayload is the slice of GitHub's push event we act on.
type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// githubWorkflowPayload is the slice of GitHub's workflow_run / check_suite
// events the board acts on.
//
// Both carry the same three facts under different keys — a head commit, a
// completion state and the repository — so one struct reads both: workflow_run
// puts them under "workflow_run", check_suite under "check_suite", and the
// unmarshal simply leaves the absent one zero. Whichever is populated wins.
type githubWorkflowPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	WorkflowRun struct {
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"workflow_run"`
	CheckSuite struct {
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"check_suite"`
}

// headSHA returns whichever of the two sub-objects carried the commit.
func (p githubWorkflowPayload) headSHA() string {
	if p.WorkflowRun.HeadSHA != "" {
		return p.WorkflowRun.HeadSHA
	}
	return p.CheckSuite.HeadSHA
}

// status returns whichever of the two sub-objects carried the run state.
func (p githubWorkflowPayload) status() string {
	if p.WorkflowRun.HeadSHA != "" {
		return p.WorkflowRun.Status
	}
	return p.CheckSuite.Status
}

// GitHubWebhook — POST /v1/github/webhook (public path, no bearer auth).
// The endpoint is reachable by anyone, so the per-repo HMAC signature is the
// entire authentication story: nothing that costs anything (a pull, an
// embedding call, a GitHub round-trip) may happen before the signature over the
// raw body verifies.
//
// The body is read here, after the middleware chain, and the middleware chain
// is required not to have touched it: the HMAC is computed over the exact bytes
// GitHub sent, so anything that buffered, re-encoded or normalised the body
// upstream would turn every genuine delivery into a signature failure.
//
// Three event types are handled, and every one of them goes through the SAME
// resolve-then-verify order — parse enough to name the repository, resolve the
// repository to get its secret, verify, and only then act.
func (h *Handler) GitHubWebhook(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "repository service not configured", Type: "service_unavailable"},
		})
	}
	switch c.Get("X-GitHub-Event") {
	case "push":
		return h.githubPushEvent(c)
	case "workflow_run", "check_suite":
		return h.githubWorkflowEvent(c)
	case "issues":
		return h.githubIssuesEvent(c)
	default:
		// ping, installation, etc. — acknowledged, ignored.
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (h *Handler) githubPushEvent(c *fiber.Ctx) error {
	body := c.Body()
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Repository.FullName == "" {
		return badRequest(c, "invalid push payload")
	}

	repo, ok, err := h.verifiedWebhookRepo(c, payload.Repository.FullName, body)
	if err != nil || !ok {
		return err
	}

	accepted, reason := h.repositorySvc.HandleGitHubPush(
		c.UserContext(), repo.ID, c.Get("X-GitHub-Delivery"),
		payload.Ref, payload.Repository.DefaultBranch, payload.After,
	)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"accepted": accepted, "reason": reason})
}

// githubWorkflowEvent handles a finished Actions run (or check suite) — the
// signal the code-review gate was built around and never received, because the
// hook was registered for push only.
func (h *Handler) githubWorkflowEvent(c *fiber.Ctx) error {
	body := c.Body()
	var payload githubWorkflowPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Repository.FullName == "" {
		return badRequest(c, "invalid workflow payload")
	}

	repo, ok, err := h.verifiedWebhookRepo(c, payload.Repository.FullName, body)
	if err != nil || !ok {
		return err
	}

	accepted, reason := h.repositorySvc.HandleGitHubWorkflowEvent(
		c.UserContext(), repo.ID, c.Get("X-GitHub-Delivery"),
		payload.Action, payload.status(), payload.headSHA(),
	)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"accepted": accepted, "reason": reason})
}

// githubIssuesPayload: pull_request is present (any value, even null) exactly
// when GitHub is describing a pull request through the issues API.
type githubIssuesPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue struct {
		Number int `json:"number"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		PullRequest *json.RawMessage `json:"pull_request"`
	} `json:"issue"`
}

// githubIssuesEvent only resolves and verifies the delivery; whether the issue
// is imported is issuesync's call.
func (h *Handler) githubIssuesEvent(c *fiber.Ctx) error {
	body := c.Body()
	var payload githubIssuesPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Repository.FullName == "" {
		return badRequest(c, "invalid issues payload")
	}
	repo, ok, err := h.verifiedWebhookRepo(c, payload.Repository.FullName, body)
	if err != nil || !ok {
		return err
	}
	if h.issueSyncSvc == nil || payload.Issue.PullRequest != nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	labels := make([]string, 0, len(payload.Issue.Labels))
	for _, l := range payload.Issue.Labels {
		labels = append(labels, l.Name)
	}
	key := payload.Repository.FullName + "#" + strconv.Itoa(payload.Issue.Number)
	accepted, reason := h.issueSyncSvc.HandleGitHubIssuesEvent(c.UserContext(), repo.ID, payload.Action, key, labels)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"accepted": accepted, "reason": reason})
}

// verifiedWebhookRepo resolves the delivery's repository and checks the HMAC
// over the raw body. It returns (repo, true, nil) only when both succeed;
// otherwise the returned error is the response already written (204 for a repo
// we do not know, 401 for a bad signature) and the caller must return it
// unchanged.
//
// It is one function shared by every event type on purpose: this is the whole
// authentication story for a public endpoint, and an event handler that
// forgot to call it would be an unauthenticated write path.
func (h *Handler) verifiedWebhookRepo(c *fiber.Ctx, fullName string, body []byte) (repo domainRepositoryRef, ok bool, err error) {
	resolved, secret, found := h.repositorySvc.ResolvePushTarget(c.UserContext(), fullName)
	if !found {
		// Not our repo: acknowledge so GitHub does not retry, start nothing.
		return repo, false, c.SendStatus(fiber.StatusNoContent)
	}
	signature := c.Get("X-Hub-Signature-256")
	if !verifyGitHubSignature(secret, body, signature) {
		log.Warn().
			Str("repository_id", resolved.ID.String()).
			Str("full_name", fullName).
			Str("event", c.Get("X-GitHub-Event")).
			Bool("signature_present", signature != "").
			Bool("secret_stored", secret != "").
			Msg("github webhook rejected: signature verification failed")
		return repo, false, c.Status(fiber.StatusUnauthorized).JSON(errorResponse{
			Error: errorDetail{Message: "webhook signature verification failed", Type: "authentication_error"},
		})
	}
	return domainRepositoryRef{ID: resolved.ID}, true, nil
}

// domainRepositoryRef is the only thing the webhook handlers need out of a
// resolved repository. Named rather than passing the whole domain.Repository so
// no handler is tempted to act on repository state it did not re-read.
type domainRepositoryRef struct {
	ID uuid.UUID
}

// verifyGitHubSignature checks GitHub's "sha256=<hex hmac>" header over the
// raw request body. Constant-time compare; empty secret or header never passes.
func verifyGitHubSignature(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(header), []byte(expected))
}

// SetupRepositoryWebhook — POST /v1/repositories/:id/webhook
// One-click install/rotate of the repo's GitHub push webhook.
func (h *Handler) SetupRepositoryWebhook(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	repo, err := h.repositorySvc.SetupWebhook(h.enrichContext(c), id)
	if err != nil {
		logWebhookSetupFailure(id, err)
		return badRequest(c, err.Error())
	}
	return c.JSON(repo)
}

// logWebhookSetupFailure records a failed "Set up webhook" click server-side.
// The error chain from repository.Service.SetupWebhook already carries the
// GitHub API status code and response body (see githubapi.EnsureRepoWebhook /
// apiError.Error), so logging it whole is enough to tell a real GitHub
// rejection apart from every other failure mode without a bespoke type
// assertion on an unexported error type from another package.
func logWebhookSetupFailure(repositoryID uuid.UUID, err error) {
	log.Error().
		Err(err).
		Str("repository_id", repositoryID.String()).
		Msg("webhook setup failed")
}
