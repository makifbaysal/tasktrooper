package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// RunnersAPI registers this computer as a self-hosted runner. Every runner is
// a just-in-time one: GitHub hands back a config good for one job, after
// which the runner removes itself, so nothing long-lived is stored here.
type RunnersAPI struct {
	token   TokenSource
	baseURL string
}

func NewRunnersAPI(source TokenSource) *RunnersAPI {
	return &RunnersAPI{token: source}
}

func (a *RunnersAPI) SetBaseURL(u string) { a.baseURL = u }

// ErrNotConnected is a call made with no GitHub connection at all.
var ErrNotConnected = errors.New("GitHub is not connected")

func (a *RunnersAPI) resolve(ctx context.Context) (string, error) {
	if a == nil || a.token == nil {
		return "", ErrNotConnected
	}
	token, err := a.token(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", ErrNotConnected
	}
	return token, nil
}

type RepoAccess struct {
	Private bool
	Admin   bool
}

func (a *RunnersAPI) RepoAccess(ctx context.Context, owner, repo string) (RepoAccess, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return RepoAccess{}, err
	}
	var out struct {
		Private     bool `json:"private"`
		Permissions struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, repoPath(owner, repo), nil, &out); err != nil {
		return RepoAccess{}, err
	}
	return RepoAccess{Private: out.Private, Admin: out.Permissions.Admin}, nil
}

type JITRunner struct {
	ID            int64
	EncodedConfig string
}

// repoRunnerGroup is the only group a repository-level runner can join.
const repoRunnerGroup = 1

func (a *RunnersAPI) GenerateJITConfig(ctx context.Context, owner, repo, name string, labels []string, workFolder string) (JITRunner, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return JITRunner{}, err
	}
	body := map[string]any{
		"name":            name,
		"runner_group_id": repoRunnerGroup,
		"labels":          labels,
		"work_folder":     workFolder,
	}
	var out struct {
		Runner struct {
			ID int64 `json:"id"`
		} `json:"runner"`
		EncodedJITConfig string `json:"encoded_jit_config"`
	}
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodPost, repoPath(owner, repo)+"/actions/runners/generate-jitconfig", body, &out); err != nil {
		return JITRunner{}, err
	}
	if out.EncodedJITConfig == "" {
		return JITRunner{}, fmt.Errorf("github answered generate-jitconfig with no config")
	}
	return JITRunner{ID: out.Runner.ID, EncodedConfig: out.EncodedJITConfig}, nil
}

// DeleteRunner removes a registration. A runner that ran its job has already
// removed itself, so a 404 is success.
func (a *RunnersAPI) DeleteRunner(ctx context.Context, owner, repo string, id int64) error {
	token, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	err = doJSONAt(ctx, a.baseURL, token, http.MethodDelete, fmt.Sprintf("%s/actions/runners/%d", repoPath(owner, repo), id), nil, nil)
	if HTTPStatus(err) == http.StatusNotFound {
		return nil
	}
	return err
}

// HTTPStatus is the status GitHub answered a failed call with; 0 when err is
// not a GitHub answer (a network error, no connection).
func HTTPStatus(err error) int {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// APIMessage is GitHub's own sentence for a failed call, or err's text.
func APIMessage(err error) string {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
