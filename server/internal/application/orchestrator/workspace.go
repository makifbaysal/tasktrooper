package orchestrator

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Supplies projects and repositories for the toolless intake and planner, so they never ask what the store has.
type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
}

// Optional; the pipeline keeps working, it just loses the never-ask-about-repos grounding.
func (s *Service) SetWorkspace(w WorkspaceLister) {
	s.workspace = w
}

// ProjectModel is the structured project model's read surface the workspace
// snapshot enriches itself with: each project's type and each repository's
// components. Optional; without it the snapshot still lists projects and
// repositories, just without that detail.
type ProjectModel interface {
	ProjectsOverview(ctx context.Context) (domain.ProjectsOverview, error)
}

func (s *Service) SetProjectModel(m ProjectModel) {
	s.projectModel = m
}

// Lets each subtask re-read the board-action ledger instead of the run-start snapshot. Optional.
func (s *Service) SetSessionActions(r SessionActionReader) {
	if s.executor != nil {
		s.executor.actions = r
	}
}

// Errors degrade to a partial snapshot rather than failing the run — the old "one question too many" behaviour.
func (s *Service) workspaceFacts(ctx context.Context) string {
	if s.workspace == nil {
		return ""
	}
	projects, projErr := s.workspace.ListProjects(ctx)
	if projErr != nil {
		log.Warn().Err(projErr).Msg("workspace snapshot: project list failed")
	}
	repos, repoErr := s.workspace.ListRepositories(ctx)
	if repoErr != nil {
		log.Warn().Err(repoErr).Msg("workspace snapshot: repository list failed")
	}
	if projErr != nil && repoErr != nil {
		return ""
	}
	projectTypes, componentsByRepo := s.projectModelFacts(ctx)
	return prompt.WorkspaceFactsBlock(projects, repos, projectTypes, componentsByRepo)
}

// projectModelFacts degrades to nil maps on any failure: the snapshot still
// renders, it just carries no component/type detail that run.
func (s *Service) projectModelFacts(ctx context.Context) (map[uuid.UUID]domain.ProjectType, map[uuid.UUID][]domain.ComponentSummary) {
	if s.projectModel == nil {
		return nil, nil
	}
	overview, err := s.projectModel.ProjectsOverview(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("workspace snapshot: project model overview failed")
		return nil, nil
	}
	projectTypes := make(map[uuid.UUID]domain.ProjectType, len(overview.Projects))
	componentsByRepo := make(map[uuid.UUID][]domain.ComponentSummary)
	for _, p := range overview.Projects {
		projectTypes[p.ID] = p.Type
		for _, r := range p.Repositories {
			componentsByRepo[r.ID] = r.Components
		}
	}
	for _, r := range overview.Unassigned {
		componentsByRepo[r.ID] = r.Components
	}
	return projectTypes, componentsByRepo
}

func WorkspaceFactsForTest(w WorkspaceLister, m ProjectModel) string {
	s := &Service{}
	if w != nil {
		s.SetWorkspace(w)
	}
	if m != nil {
		s.SetProjectModel(m)
	}
	return s.workspaceFacts(context.Background())
}
