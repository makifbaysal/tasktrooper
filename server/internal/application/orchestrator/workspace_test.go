package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

type fakeWorkspace struct {
	projects    []domain.InitiativeProject
	repos       []domain.Repository
	projectsErr error
	reposErr    error
}

func (f fakeWorkspace) ListProjects(context.Context) ([]domain.InitiativeProject, error) {
	return f.projects, f.projectsErr
}

func (f fakeWorkspace) ListRepositories(context.Context) ([]domain.Repository, error) {
	return f.repos, f.reposErr
}

func TestWorkspaceFacts_RendersSnapshot(t *testing.T) {
	facts := orchestrator.WorkspaceFactsForTest(fakeWorkspace{
		projects: []domain.InitiativeProject{{ID: uuid.New(), Name: "Acme"}},
		repos:    []domain.Repository{{Name: "acme-web", Kind: domain.RepoKindFrontend}},
	}, nil)

	assert.Contains(t, facts, "Acme")
	assert.Contains(t, facts, "acme-web")
}

func TestWorkspaceFacts_NoListerIsEmpty(t *testing.T) {
	assert.Empty(t, orchestrator.WorkspaceFactsForTest(nil, nil))
}

func TestWorkspaceFacts_PartialFailureStillRenders(t *testing.T) {
	facts := orchestrator.WorkspaceFactsForTest(fakeWorkspace{
		projectsErr: errors.New("boom"),
		repos:       []domain.Repository{{Name: "acme-web"}},
	}, nil)

	assert.Contains(t, facts, "acme-web")
}

func TestWorkspaceFacts_TotalFailureIsEmpty(t *testing.T) {
	facts := orchestrator.WorkspaceFactsForTest(fakeWorkspace{
		projectsErr: errors.New("boom"),
		reposErr:    errors.New("boom"),
	}, nil)

	assert.Empty(t, facts)
}

type fakeProjectModel struct {
	overview domain.ProjectsOverview
	err      error
}

func (f fakeProjectModel) ProjectsOverview(context.Context) (domain.ProjectsOverview, error) {
	return f.overview, f.err
}

func TestWorkspaceFacts_AddsComponentsAndProjectTypeFromProjectModel(t *testing.T) {
	projectID := uuid.New()
	repoID := uuid.New()
	facts := orchestrator.WorkspaceFactsForTest(
		fakeWorkspace{
			projects: []domain.InitiativeProject{{ID: projectID, Name: "Acme"}},
			repos:    []domain.Repository{{ID: repoID, Name: "acme-api"}},
		},
		fakeProjectModel{overview: domain.ProjectsOverview{
			Projects: []domain.ProjectOverview{{
				ID:   projectID,
				Name: "Acme",
				Type: domain.ProjectTypeSingle,
				Repositories: []domain.RepositorySummary{{
					ID:         repoID,
					Name:       "acme-api",
					Components: []domain.ComponentSummary{{Path: ".", Role: domain.ComponentRoleBackend}},
				}},
			}},
		}},
	)

	assert.Contains(t, facts, "Acme (single_repo)")
	assert.Contains(t, facts, "role: backend")
}

func TestWorkspaceFacts_ProjectModelFailureStillRendersPlainSnapshot(t *testing.T) {
	facts := orchestrator.WorkspaceFactsForTest(
		fakeWorkspace{repos: []domain.Repository{{Name: "acme-web"}}},
		fakeProjectModel{err: errors.New("boom")},
	)

	assert.Contains(t, facts, "acme-web")
}

func TestBuildIntakeSystemPrompt_IncludesWorkspaceFacts(t *testing.T) {
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
		Lang:      "tr",
		Workspace: "## Workspace state\nRepositories (1): acme-web",
	})

	assert.Contains(t, p, "acme-web")
	assert.Contains(t, p, "Never ask the stakeholder for information the system already stores")
}

func TestBuildIntakeSystemPrompt_OmitsEmptyWorkspaceSection(t *testing.T) {
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{Lang: "tr"})

	assert.NotContains(t, p, "## Workspace state")
	assert.Contains(t, p, "Never ask the stakeholder for information the system already stores")
}

func TestBuildIntakeSystemPrompt_IncludesSoloAgentRules(t *testing.T) {
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
		Lang:                 "tr",
		SoloAgentName:        "product-manager",
		SoloAgentDescription: "PM agent",
		SoloAgentRules: []domain.OrchestratorRule{
			{Name: "look-up-before-asking", Content: "Never ask what a read tool answers."},
		},
		SoloAgentSkills: []domain.Skill{
			{Name: "stakeholder-questions", Description: "Ask only blocking product questions", Enabled: true},
		},
	})

	assert.Contains(t, p, "- look-up-before-asking: Never ask what a read tool answers.")
	assert.Contains(t, p, "- stakeholder-questions: Ask only blocking product questions")
	assert.Contains(t, p, "must not be asked")
}

func TestBuildIntakeSystemPrompt_SkipsDisabledSoloAgentSkills(t *testing.T) {
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
		Lang:          "tr",
		SoloAgentName: "product-manager",
		SoloAgentSkills: []domain.Skill{
			{Name: "retired-skill", Description: "should not appear", Enabled: false},
		},
	})

	assert.NotContains(t, p, "retired-skill")
}

func TestBuildIntakeSystemPrompt_NoDanglingCatalogReference(t *testing.T) {
	// Without a catalog the prompt must not claim skills and rules exist.
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
		Lang:          "tr",
		SoloAgentName: "product-manager",
	})

	assert.NotContains(t, p, "Follow this agent's catalog skills and rules")
	assert.NotContains(t, p, "This agent's own rules follow")
	assert.NotContains(t, p, "Skills this agent already has")
}

func TestBuildPlannerSystemPrompt_IncludesWorkspaceFacts(t *testing.T) {
	p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{
		Lang:      "tr",
		Workspace: "## Workspace state\nRepositories (1): acme-web",
	})

	assert.Contains(t, p, "acme-web")
	assert.Contains(t, p, "Never ask the user for state the system already stores")
}
