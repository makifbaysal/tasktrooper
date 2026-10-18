package executorapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/ghrunner"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
)

// The GitHub Actions runner of this computer, for a cloud that holds the
// GitHub connection but must not run a repository's code. The cloud decides
// what should run and mints each runner's one-job config; these four calls
// install the runner, start it with that config, say how it is doing, and
// stop it. Each is a document: a runner lives under its own life here, so no
// call lasts as long as it does, and a cloud that restarts finds it going.
const (
	PathGitHubRunnerPrepare = "/exec/ghrunner.prepare"
	PathGitHubRunnerStart   = "/exec/ghrunner.start"
	PathGitHubRunnerStatus  = "/exec/ghrunner.status"
	PathGitHubRunnerStop    = "/exec/ghrunner.stop"
)

// GitHubRunnerHost is what runs the runners.
type GitHubRunnerHost interface {
	Prepare(repo string) error
	Start(repo, encodedJIT string) error
	Stop(repo string, remove bool)
	Status() ghrunner.HostStatus
}

type ghRunnerRepo struct {
	Repo string `json:"repo"`
}

type ghRunnerStart struct {
	Repo string `json:"repo"`
	// JITConfig is the base64 config a runner registers itself with. It is a
	// secret for as long as the runner has not started: it is never echoed.
	JITConfig string `json:"jit_config"`
}

type ghRunnerStop struct {
	Repo   string `json:"repo"`
	Remove bool   `json:"remove,omitempty"`
}

func (h *Handler) runnerHost(w http.ResponseWriter) GitHubRunnerHost {
	if h.runners == nil {
		h.writeError(w, &executor.Failure{Code: executor.CodeNotReady, Message: "this computer's executor has no data directory to keep a GitHub runner in"})
		return nil
	}
	return h.runners
}

func validRepo(w http.ResponseWriter, h *Handler, repo string) bool {
	repo = strings.TrimSpace(repo)
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.ContainsAny(repo, " \t\r\n\\") || strings.Contains(repo, "..") || strings.Count(repo, "/") != 1 {
		h.writeError(w, &executor.Failure{Code: executor.CodeBadRequest, Message: "repo must be owner/name"})
		return false
	}
	return true
}

func (h *Handler) ghRunnerPrepare(w http.ResponseWriter, r *http.Request) {
	host := h.runnerHost(w)
	if host == nil {
		return
	}
	var req ghRunnerRepo
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	if !validRepo(w, h, req.Repo) {
		return
	}
	if err := host.Prepare(strings.TrimSpace(req.Repo)); err != nil {
		h.writeError(w, &executor.Failure{Code: executor.CodeNotReady, Message: err.Error()})
		return
	}
	h.writeJSON(w, http.StatusOK, host.Status())
}

func (h *Handler) ghRunnerStart(w http.ResponseWriter, r *http.Request) {
	host := h.runnerHost(w)
	if host == nil {
		return
	}
	var req ghRunnerStart
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	if !validRepo(w, h, req.Repo) {
		return
	}
	// The config is the one secret in this exchange; redact it from anything
	// this answer might quote.
	if err := host.Start(strings.TrimSpace(req.Repo), req.JITConfig); err != nil {
		code := executor.CodeInternal
		switch {
		case errors.Is(err, ghrunner.ErrHostBusy):
			code = executor.CodeConflict
		case errors.Is(err, ghrunner.ErrHostNotReady):
			code = executor.CodeNotReady
		case strings.Contains(err.Error(), "needs its config"):
			code = executor.CodeBadRequest
		}
		h.writeError(w, &executor.Failure{Code: code, Message: err.Error()}, req.JITConfig)
		return
	}
	h.writeJSON(w, http.StatusOK, host.Status())
}

func (h *Handler) ghRunnerStatus(w http.ResponseWriter, _ *http.Request) {
	host := h.runnerHost(w)
	if host == nil {
		return
	}
	h.writeJSON(w, http.StatusOK, host.Status())
}

func (h *Handler) ghRunnerStop(w http.ResponseWriter, r *http.Request) {
	host := h.runnerHost(w)
	if host == nil {
		return
	}
	var req ghRunnerStop
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	if !validRepo(w, h, req.Repo) {
		return
	}
	host.Stop(strings.TrimSpace(req.Repo), req.Remove)
	h.writeJSON(w, http.StatusOK, host.Status())
}
