// Package designsystem keeps each project's base design system and each
// repository's layer on top of it, opens the design tasks that derive them
// from existing code, and approves a task's proposals when the human approves
// the task.
package designsystem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ProjectReader interface {
	Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error)
}

type RepositoryReader interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// TaskAssigner is the part of the board a TaskCreator may also offer: handing
// an open design task to the designer that was not there when it was opened.
type TaskAssigner interface {
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
}

type DesignerResolver interface {
	AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error)
}

const (
	maxDesignTextBytes  = 200_000
	maxTokenBytes       = 200_000
	contextExcerptRunes = 3000
)

type Service struct {
	store    port.DesignSystemStore
	projects ProjectReader
	repos    RepositoryReader
	tasks    TaskCreator
	roles    DesignerResolver
}

func NewService(store port.DesignSystemStore, projects ProjectReader, repos RepositoryReader) *Service {
	return &Service{store: store, projects: projects, repos: repos}
}

func (s *Service) SetTaskCreator(t TaskCreator)       { s.tasks = t }
func (s *Service) SetRoleResolver(r DesignerResolver) { s.roles = r }

// TaskRef is the design task a target's Design System tab points at.
type TaskRef struct {
	ID           uuid.UUID         `json:"id"`
	RepositoryID uuid.UUID         `json:"repository_id"`
	Key          string            `json:"key"`
	Title        string            `json:"title"`
	Column       domain.TaskColumn `json:"column"`
	Open         bool              `json:"open"`
	// WaitingForDesigner: open, and no agent holds it yet.
	WaitingForDesigner bool `json:"waiting_for_designer,omitempty"`
}

type RepositoryLayerSummary struct {
	ID                  uuid.UUID            `json:"id"`
	Name                string               `json:"name"`
	Kind                string               `json:"kind"`
	Layer               *domain.DesignSystem `json:"layer,omitempty"`
	PendingLayer        bool                 `json:"pending_layer"`
	BuildsOnThisProject bool                 `json:"builds_on_this_project"`
	Ambiguous           bool                 `json:"ambiguous"`
}

type ProjectView struct {
	Project      domain.InitiativeProject `json:"project"`
	Current      *domain.DesignSystem     `json:"current,omitempty"`
	Pending      []domain.DesignSystem    `json:"pending"`
	Versions     []domain.DesignSystem    `json:"versions"`
	Repositories []RepositoryLayerSummary `json:"repositories"`
	Request      *TaskRef                 `json:"request,omitempty"`
}

type ProjectChoice struct {
	Project     domain.InitiativeProject `json:"project"`
	BaseVersion int                      `json:"base_version,omitempty"`
}

type RepositoryView struct {
	// Lint checks the merged tokens: a layer can break a contrast pair the
	// base had right.
	Lint           []domain.DesignLintFinding   `json:"lint"`
	RepositoryID   uuid.UUID                    `json:"repository_id"`
	RepositoryName string                       `json:"repository_name"`
	RepositoryKind string                       `json:"repository_kind"`
	Effective      domain.EffectiveDesignSystem `json:"effective"`
	BaseProjectID  *uuid.UUID                   `json:"base_project_id,omitempty"`
	ProjectChoices []ProjectChoice              `json:"project_choices"`
	PendingLayers  []domain.DesignSystem        `json:"pending_layers"`
	LayerVersions  []domain.DesignSystem        `json:"layer_versions"`
	Overrides      []string                     `json:"overrides"`
	Request        *TaskRef                     `json:"request,omitempty"`
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.DesignSystem, error) {
	ds, err := s.store.Get(ctx, id)
	if err != nil {
		return ds, err
	}
	return linted(ds), nil
}

func linted(d domain.DesignSystem) domain.DesignSystem {
	d.Lint = domain.LintDesignSystem(d.Scope, d.DesignMD, d.Tokens)
	return d
}

func lintedAll(list []domain.DesignSystem) []domain.DesignSystem {
	out := make([]domain.DesignSystem, len(list))
	for i, d := range list {
		out[i] = linted(d)
	}
	return out
}

func lintedPtr(d *domain.DesignSystem) *domain.DesignSystem {
	if d == nil {
		return nil
	}
	l := linted(*d)
	return &l
}

func approvedOf(list []domain.DesignSystem) *domain.DesignSystem {
	for i := range list {
		if list[i].Status == domain.DesignSystemApproved {
			d := list[i]
			return &d
		}
	}
	return nil
}

func pendingOf(list []domain.DesignSystem) []domain.DesignSystem {
	out := []domain.DesignSystem{}
	for _, d := range list {
		if d.Status == domain.DesignSystemInReview {
			out = append(out, d)
		}
	}
	return out
}

func (s *Service) ProjectView(ctx context.Context, projectID uuid.UUID) (ProjectView, error) {
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	versions, err := s.store.ListForProject(ctx, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	versions = lintedAll(versions)
	view := ProjectView{
		Project:      project,
		Current:      approvedOf(versions),
		Pending:      pendingOf(versions),
		Versions:     versions,
		Repositories: []RepositoryLayerSummary{},
	}
	repos, err := s.projectRepositories(ctx, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	for _, repo := range repos {
		layers, err := s.store.ListForRepository(ctx, repo.ID)
		if err != nil {
			return ProjectView{}, err
		}
		summary := RepositoryLayerSummary{
			ID:           repo.ID,
			Name:         repo.Name,
			Kind:         repo.Kind,
			Layer:        approvedOf(layers),
			PendingLayer: len(pendingOf(layers)) > 0,
		}
		baseProject, ambiguous, err := s.baseProjectFor(ctx, repo)
		if err != nil {
			return ProjectView{}, err
		}
		summary.BuildsOnThisProject = baseProject != nil && *baseProject == projectID
		summary.Ambiguous = ambiguous
		view.Repositories = append(view.Repositories, summary)
	}
	req, err := s.store.LatestRequestForProject(ctx, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	view.Request = s.taskRef(ctx, req)
	return view, nil
}

func (s *Service) RepositoryView(ctx context.Context, repositoryID uuid.UUID) (RepositoryView, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return RepositoryView{}, err
	}
	eff, err := s.effective(ctx, repo)
	if err != nil {
		return RepositoryView{}, err
	}
	layers, err := s.store.ListForRepository(ctx, repositoryID)
	if err != nil {
		return RepositoryView{}, err
	}
	explicit, err := s.store.RepositoryBaseProject(ctx, repositoryID)
	if err != nil {
		return RepositoryView{}, err
	}
	layers = lintedAll(layers)
	eff.Base, eff.Layer = lintedPtr(eff.Base), lintedPtr(eff.Layer)
	view := RepositoryView{
		Lint:           domain.LintDesignSystem(domain.DesignSystemScopeRepository, "", eff.Tokens),
		RepositoryID:   repo.ID,
		RepositoryName: repo.Name,
		RepositoryKind: repo.Kind,
		Effective:      eff,
		BaseProjectID:  explicit,
		ProjectChoices: []ProjectChoice{},
		PendingLayers:  pendingOf(layers),
		LayerVersions:  layers,
		Overrides:      []string{},
	}
	for _, pid := range repo.ProjectIDs {
		project, err := s.projects.Get(ctx, pid)
		if err != nil {
			continue
		}
		choice := ProjectChoice{Project: project}
		versions, err := s.store.ListForProject(ctx, pid)
		if err != nil {
			return RepositoryView{}, err
		}
		if base := approvedOf(versions); base != nil {
			choice.BaseVersion = base.Version
		}
		view.ProjectChoices = append(view.ProjectChoices, choice)
	}
	if eff.Base != nil && eff.Layer != nil {
		paths, err := domain.OverriddenTokenPaths(eff.Base.Tokens, eff.Layer.Tokens)
		if err == nil && paths != nil {
			view.Overrides = paths
		}
	}
	req, err := s.store.LatestRequestForRepository(ctx, repositoryID)
	if err != nil {
		return RepositoryView{}, err
	}
	view.Request = s.taskRef(ctx, req)
	return view, nil
}

func (s *Service) Effective(ctx context.Context, repositoryID uuid.UUID) (domain.EffectiveDesignSystem, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.EffectiveDesignSystem{}, err
	}
	return s.effective(ctx, repo)
}

func (s *Service) effective(ctx context.Context, repo domain.Repository) (domain.EffectiveDesignSystem, error) {
	eff := domain.EffectiveDesignSystem{RepositoryID: repo.ID, Tokens: []byte(`{}`)}
	layers, err := s.store.ListForRepository(ctx, repo.ID)
	if err != nil {
		return eff, err
	}
	eff.Layer = approvedOf(layers)

	baseProject, ambiguous, err := s.baseProjectFor(ctx, repo)
	if err != nil {
		return eff, err
	}
	eff.Ambiguous = ambiguous
	if baseProject != nil {
		project, err := s.projects.Get(ctx, *baseProject)
		if err == nil {
			eff.Project = &project
		}
		versions, err := s.store.ListForProject(ctx, *baseProject)
		if err != nil {
			return eff, err
		}
		eff.Base = approvedOf(versions)
	}

	var base, layer []byte
	if eff.Base != nil {
		base = eff.Base.Tokens
	}
	if eff.Layer != nil {
		layer = eff.Layer.Tokens
	}
	merged, err := domain.MergeDesignTokens(base, layer)
	if err != nil {
		return eff, err
	}
	eff.Tokens = merged
	return eff, nil
}

// baseProjectFor picks the project whose base a repository builds on: the
// explicit choice while the repository still belongs to that project, else
// the only linked project that has an approved base. Two or more candidates
// and no choice is ambiguous: no base applies.
func (s *Service) baseProjectFor(ctx context.Context, repo domain.Repository) (*uuid.UUID, bool, error) {
	explicit, err := s.store.RepositoryBaseProject(ctx, repo.ID)
	if err != nil {
		return nil, false, err
	}
	if explicit != nil && slices.Contains(repo.ProjectIDs, *explicit) {
		return explicit, false, nil
	}
	var candidates []uuid.UUID
	for _, pid := range repo.ProjectIDs {
		versions, err := s.store.ListForProject(ctx, pid)
		if err != nil {
			return nil, false, err
		}
		if approvedOf(versions) != nil {
			candidates = append(candidates, pid)
		}
	}
	switch len(candidates) {
	case 0:
		if len(repo.ProjectIDs) == 1 {
			only := repo.ProjectIDs[0]
			return &only, false, nil
		}
		return nil, false, nil
	case 1:
		return &candidates[0], false, nil
	default:
		return nil, true, nil
	}
}

func (s *Service) SetRepositoryBaseProject(ctx context.Context, repositoryID uuid.UUID, projectID *uuid.UUID) (RepositoryView, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return RepositoryView{}, err
	}
	if projectID != nil && !slices.Contains(repo.ProjectIDs, *projectID) {
		return RepositoryView{}, fmt.Errorf("%w: the repository does not belong to project %s", domain.ErrDesignSystemInvalid, projectID)
	}
	if err := s.store.SetRepositoryBaseProject(ctx, repositoryID, projectID); err != nil {
		return RepositoryView{}, err
	}
	return s.RepositoryView(ctx, repositoryID)
}

func (s *Service) projectRepositories(ctx context.Context, projectID uuid.UUID) ([]domain.Repository, error) {
	all, err := s.repos.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []domain.Repository{}
	for _, repo := range all {
		if slices.Contains(repo.ProjectIDs, projectID) {
			out = append(out, repo)
		}
	}
	return out, nil
}

func (s *Service) taskRef(ctx context.Context, req *domain.DesignSystemRequest) *TaskRef {
	if req == nil || s.tasks == nil {
		return nil
	}
	task, err := s.tasks.GetTask(ctx, req.TaskRepositoryID, req.TaskID)
	if err != nil {
		return nil
	}
	return &TaskRef{
		ID:           task.ID,
		RepositoryID: req.TaskRepositoryID,
		Key:          task.Key,
		Title:        task.Title,
		Column:       task.Column,
		Open:         taskOpen(task.Column),

		WaitingForDesigner: taskOpen(task.Column) && task.AssigneeAgentID == nil,
	}
}

func taskOpen(col domain.TaskColumn) bool {
	return col != domain.TaskColumnDone && col != domain.TaskColumnReleased
}

// RequestResult is the design task a request opened, or the one already
// running for the same target.
type RequestResult struct {
	Task    domain.BoardTask `json:"task"`
	Created bool             `json:"created"`
	// WaitingForDesigner is set while the task has no assignee: no agent holds
	// the designer role yet (a catalog sync may still be adding it), and the
	// task waits for one instead of going to whoever watches its column.
	WaitingForDesigner bool `json:"waiting_for_designer,omitempty"`
}

func newRequestResult(task domain.BoardTask, created bool) RequestResult {
	return RequestResult{Task: task, Created: created, WaitingForDesigner: task.AssigneeAgentID == nil}
}

// RequestForProject opens a design task that derives (or updates) the
// project's base and the layers its repositories need from the code they
// already have. A request whose task is still open is returned instead.
func (s *Service) RequestForProject(ctx context.Context, projectID uuid.UUID, notes string) (RequestResult, error) {
	if s.tasks == nil {
		return RequestResult{}, errors.New("board is not available")
	}
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return RequestResult{}, err
	}
	if res, ok := s.openRequest(ctx, s.store.LatestRequestForProject, projectID); ok {
		return res, nil
	}
	repos, err := s.projectRepositories(ctx, projectID)
	if err != nil {
		return RequestResult{}, err
	}
	if len(repos) == 0 {
		return RequestResult{}, domain.ErrDesignSystemNoRepository
	}
	versions, err := s.store.ListForProject(ctx, projectID)
	if err != nil {
		return RequestResult{}, err
	}
	in := projectTaskInput{
		ProjectName:        project.Name,
		ProjectID:          project.ID.String(),
		ProjectDescription: strings.TrimSpace(project.Description),
		Notes:              strings.TrimSpace(notes),
	}
	if current := approvedOf(versions); current != nil {
		in.CurrentVersion = current.Version
	}
	for _, repo := range repos {
		br := briefRepository{Name: repo.Name, ID: repo.ID.String(), Kind: repo.Kind, RootPath: repo.RootPath}
		layers, err := s.store.ListForRepository(ctx, repo.ID)
		if err != nil {
			return RequestResult{}, err
		}
		if layer := approvedOf(layers); layer != nil {
			br.LayerVersion = layer.Version
		}
		in.Repositories = append(in.Repositories, br)
	}
	host := hostRepository(repos)
	title := "Design system: " + project.Name
	if in.CurrentVersion > 0 {
		title = "Update design system: " + project.Name
	}
	task, err := s.tasks.CreateTask(ctx, host.ID, domain.CreateBoardTaskRequest{
		Title:               title,
		TaskType:            domain.TaskTypeDesign,
		Description:         projectTaskKey.Render(in),
		InitiativeProjectID: &projectID,
		Column:              domain.TaskColumnTodo,
		Priority:            domain.TaskPriorityMedium,
		CreatedBy:           "system",
	})
	if err != nil {
		return RequestResult{}, err
	}
	if _, err := s.store.CreateRequest(ctx, domain.DesignSystemRequest{
		Scope: domain.DesignSystemScopeProject, ProjectID: &projectID, TaskID: task.ID, TaskRepositoryID: host.ID,
	}); err != nil {
		return RequestResult{}, err
	}
	return newRequestResult(task, true), nil
}

// RequestForRepository opens a design task for one repository's layer, or for
// its whole design system when it has no project base.
func (s *Service) RequestForRepository(ctx context.Context, repositoryID uuid.UUID, notes string) (RequestResult, error) {
	if s.tasks == nil {
		return RequestResult{}, errors.New("board is not available")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return RequestResult{}, err
	}
	if res, ok := s.openRequest(ctx, s.store.LatestRequestForRepository, repositoryID); ok {
		return res, nil
	}
	eff, err := s.effective(ctx, repo)
	if err != nil {
		return RequestResult{}, err
	}
	in := repositoryTaskInput{
		RepositoryName: repo.Name,
		RepositoryID:   repo.ID.String(),
		RepositoryKind: repo.Kind,
		Ambiguous:      eff.Ambiguous,
		Notes:          strings.TrimSpace(notes),
	}
	if eff.Project != nil {
		in.ProjectName = eff.Project.Name
	}
	if eff.Base != nil {
		in.BaseVersion = eff.Base.Version
	}
	if eff.Layer != nil {
		in.LayerVersion = eff.Layer.Version
	}
	req := domain.CreateBoardTaskRequest{
		Title:       "Design system layer: " + repo.Name,
		TaskType:    domain.TaskTypeDesign,
		Description: repositoryTaskKey.Render(in),
		Column:      domain.TaskColumnTodo,
		Priority:    domain.TaskPriorityMedium,
		CreatedBy:   "system",
	}
	if eff.Project != nil {
		pid := eff.Project.ID
		req.InitiativeProjectID = &pid
	}
	task, err := s.tasks.CreateTask(ctx, repo.ID, req)
	if err != nil {
		return RequestResult{}, err
	}
	if _, err := s.store.CreateRequest(ctx, domain.DesignSystemRequest{
		Scope: domain.DesignSystemScopeRepository, RepositoryID: &repositoryID, TaskID: task.ID, TaskRepositoryID: repo.ID,
	}); err != nil {
		return RequestResult{}, err
	}
	return newRequestResult(task, true), nil
}

func (s *Service) openRequest(ctx context.Context, latest func(context.Context, uuid.UUID) (*domain.DesignSystemRequest, error), id uuid.UUID) (RequestResult, bool) {
	req, err := latest(ctx, id)
	if err != nil || req == nil {
		return RequestResult{}, false
	}
	task, err := s.tasks.GetTask(ctx, req.TaskRepositoryID, req.TaskID)
	if err != nil || !taskOpen(task.Column) {
		return RequestResult{}, false
	}
	return newRequestResult(s.assignDesigner(ctx, req.TaskRepositoryID, task), false), true
}

// assignDesigner hands an open design task that nobody holds to the designer,
// when one exists by now: pressing the button again is how a person asks for
// the task they opened before the designer agent arrived.
func (s *Service) assignDesigner(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) domain.BoardTask {
	assigner, ok := s.tasks.(TaskAssigner)
	if task.AssigneeAgentID != nil || s.roles == nil || !ok {
		return task
	}
	designer, err := s.roles.AssigneeForNewTask(ctx, domain.TaskTypeDesign, "", nil)
	if err != nil || designer == nil {
		return task
	}
	updated, err := assigner.UpdateTask(ctx, repositoryID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.SetNullable(*designer),
	})
	if err != nil {
		return task
	}
	return updated
}

// hostRepository is where a project-wide design task lives: a UI repository
// when the project has one, so the designer's workspace holds UI code.
func hostRepository(repos []domain.Repository) domain.Repository {
	for _, kind := range []string{domain.RepoKindFrontend, domain.RepoKindMobile, domain.RepoKindMonorepo} {
		for _, repo := range repos {
			if repo.Kind == kind {
				return repo
			}
		}
	}
	return repos[0]
}

// Propose records a design task's proposal for one target. A second proposal
// from the same task for the same target replaces its pending version rather
// than adding another, so a revision round never piles versions up.
func (s *Service) Propose(ctx context.Context, p domain.DesignSystemProposal) (domain.DesignSystem, error) {
	if p.TaskID == uuid.Nil {
		return domain.DesignSystem{}, fmt.Errorf("%w: a design system is proposed from a design task", domain.ErrDesignSystemInvalid)
	}
	if !domain.ValidDesignSystemScope(p.Scope) {
		return domain.DesignSystem{}, fmt.Errorf("%w: scope must be %q or %q", domain.ErrDesignSystemInvalid, domain.DesignSystemScopeProject, domain.DesignSystemScopeRepository)
	}
	designMD := domain.TrimDesignText(p.DesignMD)
	inventory := domain.TrimDesignText(p.InventoryMD)
	rationale := domain.TrimDesignText(p.Rationale)
	if len(designMD) > maxDesignTextBytes || len(inventory) > maxDesignTextBytes {
		return domain.DesignSystem{}, fmt.Errorf("%w: design_md and inventory_md are limited to %d bytes each", domain.ErrDesignSystemInvalid, maxDesignTextBytes)
	}
	if len(p.Tokens) > maxTokenBytes {
		return domain.DesignSystem{}, fmt.Errorf("%w: tokens are limited to %d bytes", domain.ErrDesignSystemInvalid, maxTokenBytes)
	}
	tokens, err := domain.ParseDesignTokens(p.Tokens)
	if err != nil {
		return domain.DesignSystem{}, err
	}

	ds := domain.DesignSystem{
		Scope:         p.Scope,
		Status:        domain.DesignSystemInReview,
		DesignMD:      designMD,
		Tokens:        tokens,
		InventoryMD:   inventory,
		Rationale:     rationale,
		SourceTaskKey: p.TaskKey,
		CreatedBy:     p.CreatedBy,
	}
	taskID := p.TaskID
	ds.SourceTaskID = &taskID

	switch p.Scope {
	case domain.DesignSystemScopeProject:
		if p.ProjectID == nil {
			return domain.DesignSystem{}, fmt.Errorf("%w: project_id is required for scope %q", domain.ErrDesignSystemInvalid, p.Scope)
		}
		if _, err := s.projects.Get(ctx, *p.ProjectID); err != nil {
			return domain.DesignSystem{}, err
		}
		if designMD == "" {
			return domain.DesignSystem{}, fmt.Errorf("%w: a project base needs design_md", domain.ErrDesignSystemInvalid)
		}
		ds.ProjectID = p.ProjectID
	case domain.DesignSystemScopeRepository:
		if p.RepositoryID == nil {
			return domain.DesignSystem{}, fmt.Errorf("%w: repository_id is required for scope %q", domain.ErrDesignSystemInvalid, p.Scope)
		}
		if _, err := s.repos.Get(ctx, *p.RepositoryID); err != nil {
			return domain.DesignSystem{}, err
		}
		if rationale == "" {
			return domain.DesignSystem{}, fmt.Errorf("%w: a repository layer needs a rationale — why this repository differs from the base", domain.ErrDesignSystemInvalid)
		}
		if err := domain.CheckLayerTokens(tokens); err != nil {
			return domain.DesignSystem{}, err
		}
		if designMD == "" && string(tokens) == "{}" {
			return domain.DesignSystem{}, fmt.Errorf("%w: a repository layer needs tokens or design_md", domain.ErrDesignSystemInvalid)
		}
		ds.RepositoryID = p.RepositoryID
	}

	existing, err := s.store.ListBySourceTask(ctx, taskID)
	if err != nil {
		return domain.DesignSystem{}, err
	}
	for _, prev := range existing {
		if prev.Status == domain.DesignSystemInReview && prev.Scope == ds.Scope && prev.TargetID() == ds.TargetID() {
			ds.ID = prev.ID
			saved, err := s.store.UpdateContent(ctx, ds)
			if err != nil {
				return saved, err
			}
			return linted(saved), nil
		}
	}
	saved, err := s.store.Create(ctx, ds)
	if err != nil {
		return saved, err
	}
	return linted(saved), nil
}

// Files renders a repository's effective design system as the files the
// repository keeps: DESIGN.md, the merged token tree, its CSS variables and
// the component inventory. None when the repository follows no design system.
func (s *Service) Files(ctx context.Context, repositoryID uuid.UUID) ([]domain.DesignSystemFile, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	eff, err := s.effective(ctx, repo)
	if err != nil {
		return nil, err
	}
	files := []domain.DesignSystemFile{}
	if eff.Empty() {
		return files, nil
	}
	in := fileInput{RepositoryName: repo.Name}
	if eff.Project != nil {
		in.ProjectName = eff.Project.Name
	}
	if eff.Base != nil {
		in.BaseVersion, in.BaseDesignMD, in.BaseInventory = eff.Base.Version, eff.Base.DesignMD, eff.Base.InventoryMD
	}
	if eff.Layer != nil {
		in.LayerVersion, in.LayerRationale = eff.Layer.Version, eff.Layer.Rationale
		in.LayerDesignMD, in.LayerInventory = eff.Layer.DesignMD, eff.Layer.InventoryMD
	}
	tokens, err := domain.PrettyDesignTokens(eff.Tokens)
	if err != nil {
		return nil, err
	}
	css, err := domain.DesignTokensCSS(eff.Tokens)
	if err != nil {
		return nil, err
	}
	files = append(files,
		domain.DesignSystemFile{Path: domain.DesignFileDesignMD, Content: strings.TrimLeft(designMDFileKey.Render(in), "\n")},
		domain.DesignSystemFile{Path: domain.DesignFileTokens, Content: tokens},
		domain.DesignSystemFile{Path: domain.DesignFileCSS, Content: css},
	)
	if in.BaseInventory != "" || in.LayerInventory != "" {
		files = append(files, domain.DesignSystemFile{Path: domain.DesignFileInventory, Content: strings.TrimLeft(inventoryFileKey.Render(in), "\n")})
	}
	return files, nil
}

// ApproveForTask approves every pending version the task proposed. It runs
// when a design task enters a stage carrying approve_design_system_on_enter,
// and never fails the move that triggered it.
func (s *Service) ApproveForTask(ctx context.Context, taskID uuid.UUID) []domain.DesignSystem {
	proposals, err := s.store.ListBySourceTask(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("design system: list task proposals failed")
		return nil
	}
	var approved []domain.DesignSystem
	for _, p := range proposals {
		if p.Status != domain.DesignSystemInReview {
			continue
		}
		ds, err := s.store.Approve(ctx, p.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Str("design_system_id", p.ID.String()).
				Msg("design system: approve failed")
			continue
		}
		approved = append(approved, ds)
	}
	return approved
}

// ContextNote is the design system block every board run in the repository
// carries; "" when the repository follows none.
func (s *Service) ContextNote(ctx context.Context, repositoryID uuid.UUID, showTool bool) string {
	eff, err := s.Effective(ctx, repositoryID)
	if err != nil || eff.Empty() {
		return ""
	}
	in := contextNoteInput{
		BaseVersion:  domain.DesignSystemLabel(eff.Base),
		LayerVersion: domain.DesignSystemLabel(eff.Layer),
		Ambiguous:    eff.Ambiguous,
		ShowTool:     showTool,
	}
	if eff.Project != nil {
		in.ProjectName = eff.Project.Name
	}
	var parts []string
	if eff.Base != nil && eff.Base.DesignMD != "" {
		parts = append(parts, eff.Base.DesignMD)
	}
	if eff.Layer != nil {
		in.LayerRationale = eff.Layer.Rationale
		if eff.Layer.DesignMD != "" {
			parts = append(parts, eff.Layer.DesignMD)
		}
	}
	in.Excerpt, in.Truncated = truncateRunes(strings.Join(parts, "\n\n"), contextExcerptRunes)
	return strings.TrimRight(contextNoteKey.Render(in), "\n")
}

func truncateRunes(s string, max int) (string, bool) {
	if utf8.RuneCountInString(s) <= max {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:max]), true
}
