// Package newrepo creates a brand-new, empty repository from what the person
// says it will be. There is no code to scan yet, so instead of a scan the
// answers become the root component and one bootstrap task that writes the
// skeleton, the reference docs and the agent instructions in a single pull
// request; the first push to the default branch scans it.
package newrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RepositoryCreator interface {
	CreateWithoutScan(ctx context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type ComponentWriter interface {
	AddComponent(ctx context.Context, repoID uuid.UUID, req domain.NewComponentRequest) (domain.Component, error)
	UpdateComponent(ctx context.Context, componentID uuid.UUID, patch domain.ComponentPatch) (domain.Component, error)
}

type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

type Result struct {
	Repository  domain.Repository
	ComponentID uuid.UUID
	// Task is nil when neither a skeleton nor any doc was asked for.
	Task *domain.BoardTask
}

// IncompleteError is a failure after the repository itself was created: it is
// registered and on GitHub, so the person keeps it and finishes the setup from
// its page rather than retrying the same name.
type IncompleteError struct {
	Repository domain.Repository
	Step       string
	Err        error
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("repository %q was created, but %s failed: %v — finish its setup from the repository page", e.Repository.Name, e.Step, e.Err)
}

func (e *IncompleteError) Unwrap() error { return e.Err }

type Service struct {
	repos      RepositoryCreator
	components ComponentWriter
	tasks      TaskCreator
	roles      port.RoleResolver
	designs    DesignSystemReader
}

func NewService(repos RepositoryCreator, components ComponentWriter, tasks TaskCreator) *Service {
	return &Service{repos: repos, components: components, tasks: tasks}
}

func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

// DesignSystemReader is what a new repository inherits from the projects it
// joins: a UI repository's setup writes their design system from the start.
type DesignSystemReader interface {
	Effective(ctx context.Context, repositoryID uuid.UUID) (domain.EffectiveDesignSystem, error)
}

func (s *Service) SetDesignSystems(d DesignSystemReader) { s.designs = d }

func (s *Service) inheritedDesignSystem(ctx context.Context, repositoryID uuid.UUID, role domain.ComponentRole) *bootstrapDesign {
	if s.designs == nil {
		return nil
	}
	switch role {
	case domain.ComponentRoleFrontend, domain.ComponentRoleMobile, domain.ComponentRoleDesktop:
	default:
		return nil
	}
	eff, err := s.designs.Effective(ctx, repositoryID)
	if err != nil || eff.Base == nil || eff.Project == nil {
		return nil
	}
	return &bootstrapDesign{ProjectName: eff.Project.Name, Version: eff.Base.Version}
}

// Create validates everything before touching disk or GitHub: a request that
// fails validation leaves nothing behind.
func (s *Service) Create(ctx context.Context, req domain.NewRepositoryRequest) (Result, error) {
	role := domain.ComponentRole(strings.TrimSpace(string(req.Role)))
	if role == "" {
		return Result{}, fmt.Errorf("role is required")
	}
	if !domain.ValidComponentRole(role) {
		return Result{}, fmt.Errorf("unknown role %q", role)
	}
	docs, err := normalizeDocs(req.Docs)
	if err != nil {
		return Result{}, err
	}
	if _, err := domain.NewRepoDirName(req.Name); err != nil {
		return Result{}, err
	}
	if s.repos == nil || s.components == nil || s.tasks == nil {
		return Result{}, fmt.Errorf("creating repositories is not available on this server")
	}

	repo, err := s.repos.CreateWithoutScan(ctx, domain.CreateRepositoryRequest{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		ProjectIDs:  req.ProjectIDs,
		Owner:       strings.TrimSpace(req.Owner),
		Kind:        role.LegacyRepoKind(),
	})
	if err != nil {
		return Result{}, err
	}

	comp, err := s.components.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: ".", Name: repo.Name, Role: role})
	if err != nil {
		return Result{}, &IncompleteError{Repository: repo, Step: "adding its root component", Err: err}
	}
	if len(docs) > 0 {
		var paths domain.RepositoryDocs
		for _, kind := range docs {
			paths.SetPath(kind, domain.DefaultRepoDocPath(kind))
		}
		if _, err := s.components.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Docs: &paths}); err != nil {
			return Result{}, &IncompleteError{Repository: repo, Step: "recording its reference docs", Err: err}
		}
	}
	// Adding the component re-projects the legacy repository columns (kind),
	// so the row read back is the one the rest of the app now sees.
	if fresh, err := s.repos.Get(ctx, repo.ID); err == nil {
		repo = fresh
	}

	result := Result{Repository: repo, ComponentID: comp.ID}
	if !req.Scaffold && len(docs) == 0 {
		return result, nil
	}
	componentID := comp.ID
	task, err := s.tasks.CreateTask(ctx, repo.ID, domain.CreateBoardTaskRequest{
		Title:           "Set up " + repo.Name,
		Description:     bootstrapDescription(repo.Name, role, req, docs, s.inheritedDesignSystem(ctx, repo.ID, role)),
		ComponentID:     &componentID,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.assignee(ctx, role),
	})
	if err != nil {
		return Result{}, &IncompleteError{Repository: repo, Step: "opening its setup task", Err: err}
	}
	result.Task = &task
	return result, nil
}

func normalizeDocs(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		kind := strings.TrimSpace(raw)
		if !domain.ValidRepoDocKind(kind) {
			return nil, fmt.Errorf("unknown doc kind %q", raw)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out, nil
}

func (s *Service) assignee(ctx context.Context, role domain.ComponentRole) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, domain.RepoArea(role.LegacyRepoKind(), nil))
	if err != nil {
		return nil
	}
	return id
}

func bootstrapDescription(name string, role domain.ComponentRole, req domain.NewRepositoryRequest, docs []string, design *bootstrapDesign) string {
	var items []bootstrapItem
	n := 0
	nextItem := func(kind, path, kindLabel string) {
		n++
		items = append(items, bootstrapItem{Number: n, Kind: kind, Path: path, KindLabel: kindLabel})
	}
	if req.Scaffold {
		nextItem("skeleton", "", "")
	}
	for _, kind := range docs {
		nextItem("doc", domain.DefaultRepoDocPath(kind), repodocs.DocKindLabel(kind))
	}
	if design != nil {
		nextItem("design", domain.DesignFileDesignMD, "")
	}
	nextItem("claude", "", "")

	bootstrapDocs := make([]bootstrapDoc, len(docs))
	for i, kind := range docs {
		path := domain.DefaultRepoDocPath(kind)
		bootstrapDocs[i] = bootstrapDoc{Path: path, Instructions: repodocs.NewRepoDocInstructions(kind, path)}
	}

	return bootstrapDescriptionKey.Render(bootstrapDescriptionInput{
		Name:        name,
		Role:        string(role),
		Description: strings.TrimSpace(req.Description),
		Stack:       strings.TrimSpace(req.Stack),
		Notes:       strings.TrimSpace(req.Notes),
		Scaffold:    req.Scaffold,
		HasDocs:     len(docs) > 0,
		Items:       items,
		Docs:        bootstrapDocs,
		Design:      design,
	})
}
