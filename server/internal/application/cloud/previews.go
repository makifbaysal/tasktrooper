package cloud

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskReader is the task lookup TaskPreviews keys a branch off.
type TaskReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// PullRequestHeads reads the head branch and commit of a task's pull request;
// empty strings when the task has none or GitHub is not connected.
type PullRequestHeads interface {
	PullRequestHead(ctx context.Context, repositoryID, taskID uuid.UUID) (branch, sha string, err error)
}

// SetTaskPreviewSources wires the task and pull-request reads TaskPreviews
// needs; heads may be nil (no GitHub), which only loses the head-commit
// preference.
func (s *Service) SetTaskPreviewSources(tasks TaskReader, heads PullRequestHeads) {
	s.taskReader = tasks
	s.prHeads = heads
}

type previewLookup struct {
	deployment domain.CloudDeployment
	found      bool
}

// TaskPreviews is each active component's per-branch preview of the task's
// branch — the PR's head branch when known, else the branch the task is
// pushed to — preferring the build of the PR's head commit.
func (s *Service) TaskPreviews(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPreviews, error) {
	if s.taskReader == nil {
		return domain.TaskPreviews{}, errors.New("task previews are not configured on this deployment")
	}
	task, err := s.taskReader.GetTask(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPreviews{}, err
	}
	out := domain.TaskPreviews{Branch: domain.TaskBranchName(task), Previews: []domain.TaskPreview{}}
	if s.prHeads != nil {
		branch, sha, err := s.prHeads.PullRequestHead(ctx, repositoryID, taskID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("cloud: reading the task's pull request head failed")
		} else {
			if branch != "" {
				out.Branch = branch
			}
			out.HeadSHA = sha
		}
	}

	envs, err := s.environments.ListEnvironments(ctx, repositoryID)
	if err != nil {
		return domain.TaskPreviews{}, err
	}
	components, err := s.components.ListComponents(ctx, repositoryID)
	if err != nil {
		return domain.TaskPreviews{}, err
	}
	active := make(map[uuid.UUID]domain.Component, len(components))
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive {
			active[c.ID] = c
		}
	}

	for _, env := range envs {
		comp, ok := active[env.ComponentID]
		if !ok || !env.PerBranch() || env.Status != domain.LinkConfirmed || !env.Bound() {
			continue
		}
		preview, err := s.taskPreview(ctx, env, comp, out.Branch, out.HeadSHA)
		if err != nil {
			return domain.TaskPreviews{}, err
		}
		out.Previews = append(out.Previews, preview)
	}
	sort.SliceStable(out.Previews, func(i, j int) bool { return out.Previews[i].ComponentName < out.Previews[j].ComponentName })
	return out, nil
}

func (s *Service) taskPreview(ctx context.Context, env domain.ComponentEnvironment, comp domain.Component, branch, sha string) (domain.TaskPreview, error) {
	none := domain.TaskPreview{
		ComponentID:   comp.ID,
		ComponentName: comp.DisplayName(),
		EnvironmentID: env.ID,
		Provider:      env.Provider,
		Status:        domain.TaskPreviewNone,
	}
	bound, err := s.bindRuntime(ctx, env)
	if err != nil {
		if errors.Is(err, ErrNotConnected) {
			return none, nil
		}
		return domain.TaskPreview{}, err
	}
	previewer, ok := bound.provider.(port.CloudPreviewer)
	if !ok {
		return none, nil
	}

	v, err := s.runtimeCached(runtimeCacheKey{env.ID, "preview", branch + "|" + sha}, func() (interface{}, error) {
		d, found, err := previewer.Preview(ctx, bound.cred, *env.Resource, branch, sha)
		return previewLookup{deployment: d, found: found}, err
	})
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *env.AccountID, err)
		}
		return domain.TaskPreview{}, fmt.Errorf("%s preview: %w", comp.DisplayName(), err)
	}
	lookup := v.(previewLookup)
	if !lookup.found {
		return none, nil
	}

	preview := domain.TaskPreviewFromDeployment(lookup.deployment)
	preview.ComponentID = none.ComponentID
	preview.ComponentName = none.ComponentName
	preview.EnvironmentID = none.EnvironmentID
	preview.Provider = none.Provider
	if access, ok := s.previewAccess(ctx, bound); ok {
		preview.Protected = access.Protected
		preview.BypassConfigured = access.BypassConfigured
		preview.BypassSecret = access.BypassSecret
	}
	return preview, nil
}
