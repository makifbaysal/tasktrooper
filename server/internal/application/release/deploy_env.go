package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// EnvGate is the pre-ship environment-variable check (*envreq.Service): it
// fills what it can itself and reports what only a human can provide.
type EnvGate interface {
	Ensure(ctx context.Context, repositoryID, componentID uuid.UUID, taskID *uuid.UUID) (domain.EnvEnsureResult, error)
}

type releaseNameNamesInput struct {
	Name  string
	Names []string
}

type releaseNameReasonInput struct{ Name, Reason string }

type envCreatedInput struct {
	Name                string
	Names               []string
	OverwrittenOnDeploy bool
}

var (
	envMissingKey        = prompt.Define("guard.release_env_missing", releaseNameNamesInput{Name: "api", Names: []string{"API_KEY"}})
	envUncheckedKey      = prompt.Define("guard.release_env_unchecked", releaseNameReasonInput{Name: "api", Reason: "token refused"})
	envMissingCommentKey = prompt.Define("notices.release_env_missing_comment", releaseNameNamesInput{Name: "api", Names: []string{"API_KEY"}})
	envUncheckedComment  = prompt.Define("notices.release_env_unchecked_comment", releaseNameReasonInput{Name: "api", Reason: "token refused"})
	envCreatedCommentKey = prompt.Define("notices.release_env_created_comment", envCreatedInput{Name: "api", Names: []string{"SESSION_SECRET"}, OverwrittenOnDeploy: true})
)

// envGate checks the component's production variables before the code that
// reads them ships. taskID is the task whose checkout is about to ship, and
// the one the comments go on.
func (s *Service) envGate(ctx context.Context, repositoryID uuid.UUID, component domain.Component, name string, taskID uuid.UUID) (domain.ResourceBlock, error) {
	if s.envs == nil {
		return domain.ResourceBlock{}, nil
	}
	res, err := s.envs.Ensure(ctx, repositoryID, component.ID, &taskID)
	if len(res.Created) > 0 {
		s.commentOnce(ctx, repositoryID, taskID, envCreatedCommentKey.Render(envCreatedInput{
			Name: name, Names: res.Created, OverwrittenOnDeploy: res.OverwrittenOnDeploy,
		}))
	}
	if err != nil {
		in := releaseNameReasonInput{Name: name, Reason: err.Error()}
		s.commentOnce(ctx, repositoryID, taskID, envUncheckedComment.Render(in))
		return domain.ResourceBlock{Resource: domain.ResourceDeployEnv},
			fmt.Errorf("%w: %s", domain.ErrDeployEnvUnchecked, envUncheckedKey.Render(in))
	}
	if len(res.Missing) > 0 {
		in := releaseNameNamesInput{Name: name, Names: res.Missing}
		s.commentOnce(ctx, repositoryID, taskID, envMissingCommentKey.Render(in))
		return domain.ResourceBlock{Resource: domain.ResourceDeployEnv, Detail: strings.Join(res.Missing, ", ")},
			fmt.Errorf("%w: %s", domain.ErrDeployEnvMissing, envMissingKey.Render(in))
	}
	return domain.ResourceBlock{}, nil
}

// ResumeEnvHolds wakes every done task of the repository whose merge waited
// on its environment variables — called once a human has entered the last of
// them. The woken run checks again, so a task of another component that is
// still missing something simply parks again.
func (s *Service) ResumeEnvHolds(ctx context.Context, repositoryID uuid.UUID) {
	if s.tasks == nil {
		return
	}
	all, err := s.tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: listing tasks to resume after env vars were set failed")
		return
	}
	for _, task := range all {
		if task.Column != domain.TaskColumnDone || task.BlockedResource != domain.ResourceDeployEnv {
			continue
		}
		s.releaseMergeHold(ctx, task, domain.ResourceDeployEnv)
		if err := s.WakeTask(ctx, repositoryID, task); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: waking a task after its env vars were set failed")
		}
	}
}

// envGateRelease is envGate for a dispatch release: the comment goes on its
// newest task and the hold on every task it carries, so ResumeEnvHolds finds
// them once the variables are set.
func (s *Service) envGateRelease(ctx context.Context, r domain.Release) error {
	if s.envs == nil || r.ComponentID == nil || len(r.Tasks) == 0 {
		return nil
	}
	component, err := s.componentByID(ctx, *r.ComponentID)
	if err != nil {
		return nil
	}
	newest := r.Tasks[len(r.Tasks)-1]
	hold, gateErr := s.envGate(ctx, r.RepositoryID, component, component.DisplayName(), newest.ID)
	if s.holds != nil {
		for _, t := range r.Tasks {
			var err error
			if hold.Resource == "" {
				err = s.holds.ReleaseMergeHold(ctx, t.ID, domain.ResourceDeployEnv)
			} else {
				err = s.holds.HoldMerge(ctx, r.RepositoryID, t.ID, hold.Resource, hold.Detail)
			}
			if err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: recording the env hold of a dispatch release failed")
			}
		}
	}
	return gateErr
}
