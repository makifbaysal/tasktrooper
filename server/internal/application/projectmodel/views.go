package projectmodel

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// listEnvironments and listAllEnvironments degrade to "none" rather than
// erroring when no reader is wired (SetEnvironmentReader not called), so
// every caller of RepositoryModel/RepositorySummary keeps working the same
// way it did before environments existed.
func (s *Service) listEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	if s.environments == nil {
		return nil, nil
	}
	return s.environments.ListEnvironments(ctx, repositoryID)
}

func (s *Service) listAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error) {
	if s.environments == nil {
		return nil, nil
	}
	return s.environments.ListAllEnvironments(ctx)
}

func environmentSummaries(environments []domain.ComponentEnvironment) []domain.EnvironmentSummary {
	out := make([]domain.EnvironmentSummary, 0, len(environments))
	for _, e := range environments {
		summary := domain.EnvironmentSummary{
			ID:          e.ID,
			ComponentID: e.ComponentID,
			Environment: e.Environment,
			Provider:    e.Provider,
			URL:         e.URL,
			Status:      e.Status,
		}
		if e.Resource != nil {
			summary.ResourceName = e.Resource.Name
		}
		if e.Health != nil {
			summary.Health = e.Health.Status
			summary.ErrorCount24h = e.Health.ErrorCount24h
		}
		out = append(out, summary)
	}
	return out
}

// RepositoryModel is the whole GET /v1/repositories/:id/model payload.
func (s *Service) RepositoryModel(ctx context.Context, repositoryID uuid.UUID) (domain.RepositoryModel, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	checks, err := s.store.ListChecks(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	links, err := s.store.ListLinks(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	incoming, err := s.store.ListIncomingLinks(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	notes, err := s.store.ListNotes(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}

	resources, err := s.store.ListResources(ctx, collectResourceIDs(links, incoming))
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	linkedComponents, err := s.linkedComponents(ctx, repositoryID, links, incoming)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	latestScan, err := s.LatestScan(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}
	environments, err := s.listEnvironments(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryModel{}, err
	}

	return domain.RepositoryModel{
		Repository:       repo,
		Shape:            domain.ShapeFromComponents(components),
		Components:       nonNil(components),
		Checks:           nonNil(checks),
		Links:            nonNil(links),
		IncomingLinks:    nonNil(incoming),
		Resources:        nonNil(resources),
		LinkedComponents: linkedComponents,
		Notes:            nonNil(notes),
		Environments:     nonNil(environments),
		Review:           nonNil(reviewItems(components, links, environments)),
		LatestScan:       latestScan,
	}, nil
}

func collectResourceIDs(linkSets ...[]domain.ComponentLink) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, set := range linkSets {
		for _, l := range set {
			if l.ToResourceID != nil && !seen[*l.ToResourceID] {
				seen[*l.ToResourceID] = true
				ids = append(ids, *l.ToResourceID)
			}
		}
	}
	return ids
}

// linkedComponents is the display identity of every OTHER repository's
// component this repository's links reference, either as an outgoing
// target or as the source of an incoming edge.
func (s *Service) linkedComponents(ctx context.Context, repositoryID uuid.UUID, links, incoming []domain.ComponentLink) ([]domain.LinkedComponent, error) {
	componentCache := map[uuid.UUID]domain.Component{}
	repoCache := map[uuid.UUID]domain.Repository{}
	seen := map[uuid.UUID]bool{}
	var out []domain.LinkedComponent

	add := func(componentID uuid.UUID) error {
		if seen[componentID] {
			return nil
		}
		comp, ok := componentCache[componentID]
		if !ok {
			var err error
			comp, err = s.store.GetComponent(ctx, componentID)
			if err != nil {
				return err
			}
			componentCache[componentID] = comp
		}
		if comp.RepositoryID == repositoryID {
			return nil
		}
		seen[componentID] = true
		repo, ok := repoCache[comp.RepositoryID]
		if !ok {
			var err error
			repo, err = s.repos.Get(ctx, comp.RepositoryID)
			if err != nil {
				return err
			}
			repoCache[comp.RepositoryID] = repo
		}
		out = append(out, domain.LinkedComponent{
			ID:             comp.ID,
			RepositoryID:   comp.RepositoryID,
			RepositoryName: repo.Name,
			ProjectIDs:     repo.ProjectIDs,
			Path:           comp.Path,
			Name:           comp.DisplayName(),
			Role:           comp.Role.Get(),
		})
		return nil
	}

	for _, l := range links {
		if l.ToComponentID != nil {
			if err := add(*l.ToComponentID); err != nil {
				return nil, err
			}
		}
	}
	for _, l := range incoming {
		if err := add(l.FromComponentID); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RepositoryName != out[j].RepositoryName {
			return out[i].RepositoryName < out[j].RepositoryName
		}
		return out[i].Path < out[j].Path
	})
	return nonNil(out), nil
}

// overviewData is every project- and repository-scoped fact ProjectsOverview
// and ProjectOverview both need, loaded once so serving either never costs
// more than one pass over the workspace.
type overviewData struct {
	repos              []domain.Repository
	componentsByRepo   map[uuid.UUID][]domain.Component
	componentRepo      map[uuid.UUID]uuid.UUID
	linksByRepo        map[uuid.UUID][]domain.ComponentLink
	allLinks           []domain.ComponentLink
	environmentsByRepo map[uuid.UUID][]domain.ComponentEnvironment
	repoProjects       map[uuid.UUID][]uuid.UUID
	resourceRepos      map[uuid.UUID]map[uuid.UUID]bool
	resourcesByID      map[uuid.UUID]domain.SystemResource
	summaries          map[uuid.UUID]domain.RepositorySummary
}

func (s *Service) loadOverviewData(ctx context.Context) (overviewData, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return overviewData{}, err
	}
	repoIDs := make([]uuid.UUID, len(repos))
	for i, r := range repos {
		repoIDs[i] = r.ID
	}

	allComponents, err := s.store.ListComponentsForRepositories(ctx, repoIDs)
	if err != nil {
		return overviewData{}, err
	}
	allLinks, err := s.store.ListLinksForRepositories(ctx, repoIDs)
	if err != nil {
		return overviewData{}, err
	}
	allEnvironments, err := s.listAllEnvironments(ctx)
	if err != nil {
		return overviewData{}, err
	}

	data := overviewData{
		repos:              repos,
		componentsByRepo:   map[uuid.UUID][]domain.Component{},
		componentRepo:      map[uuid.UUID]uuid.UUID{},
		linksByRepo:        map[uuid.UUID][]domain.ComponentLink{},
		allLinks:           allLinks,
		environmentsByRepo: map[uuid.UUID][]domain.ComponentEnvironment{},
		repoProjects:       map[uuid.UUID][]uuid.UUID{},
		resourceRepos:      map[uuid.UUID]map[uuid.UUID]bool{},
		summaries:          map[uuid.UUID]domain.RepositorySummary{},
	}
	for _, r := range repos {
		data.repoProjects[r.ID] = r.ProjectIDs
	}
	for _, e := range allEnvironments {
		data.environmentsByRepo[e.RepositoryID] = append(data.environmentsByRepo[e.RepositoryID], e)
	}
	for _, c := range allComponents {
		data.componentsByRepo[c.RepositoryID] = append(data.componentsByRepo[c.RepositoryID], c)
		data.componentRepo[c.ID] = c.RepositoryID
	}
	for _, l := range allLinks {
		data.linksByRepo[l.RepositoryID] = append(data.linksByRepo[l.RepositoryID], l)
		if l.ToResourceID != nil {
			set, ok := data.resourceRepos[*l.ToResourceID]
			if !ok {
				set = map[uuid.UUID]bool{}
				data.resourceRepos[*l.ToResourceID] = set
			}
			set[l.RepositoryID] = true
		}
	}

	resourceIDs := make([]uuid.UUID, 0, len(data.resourceRepos))
	for id := range data.resourceRepos {
		resourceIDs = append(resourceIDs, id)
	}
	resources, err := s.store.ListResources(ctx, resourceIDs)
	if err != nil {
		return overviewData{}, err
	}
	data.resourcesByID = make(map[uuid.UUID]domain.SystemResource, len(resources))
	for _, r := range resources {
		data.resourcesByID[r.ID] = r
	}

	for _, r := range repos {
		checks, err := s.store.ListChecks(ctx, r.ID)
		if err != nil {
			return overviewData{}, err
		}
		data.summaries[r.ID] = s.repositorySummary(ctx, r, data.componentsByRepo[r.ID], checks, data.linksByRepo[r.ID], data.environmentsByRepo[r.ID])
	}
	return data, nil
}

func (s *Service) repositorySummary(ctx context.Context, r domain.Repository, components []domain.Component, checks []domain.ComponentCheck, links []domain.ComponentLink, environments []domain.ComponentEnvironment) domain.RepositorySummary {
	checksByComponent := map[uuid.UUID][]domain.ComponentCheck{}
	for _, ch := range checks {
		if ch.Status != domain.ModelStatusActive || ch.Missing {
			continue
		}
		checksByComponent[ch.ComponentID] = append(checksByComponent[ch.ComponentID], ch)
	}

	var compSummaries []domain.ComponentSummary
	for _, c := range components {
		if c.Status != domain.ComponentStatusActive {
			continue
		}
		chs := checksByComponent[c.ID]
		required := 0
		for _, ch := range chs {
			if ch.Gate.Get() == domain.CheckGateRequired {
				required++
			}
		}
		compSummaries = append(compSummaries, domain.ComponentSummary{
			ID:             c.ID,
			Path:           c.Path,
			Name:           c.DisplayName(),
			Role:           c.Role.Get(),
			RoleConfidence: c.Role.Confidence,
			StackSummary:   c.Stack.Get().Summary(3),
			Checks:         len(chs),
			RequiredChecks: required,
		})
	}
	sort.Slice(compSummaries, func(i, j int) bool { return compSummaries[i].Path < compSummaries[j].Path })

	var lastScan *domain.ScanSummary
	if scan, err := s.store.LatestScan(ctx, r.ID); err == nil {
		lastScan = &domain.ScanSummary{ID: scan.ID, Status: scan.Status, Trigger: scan.Trigger, StartedAt: scan.StartedAt, FinishedAt: scan.FinishedAt}
	} else if !errors.Is(err, port.ErrNotFound) {
		log.Warn().Err(err).Str("repository_id", r.ID.String()).Msg("overview: latest scan lookup failed")
	}

	return domain.RepositorySummary{
		ID:           r.ID,
		Name:         r.Name,
		Description:  r.Description,
		RemoteURL:    r.RemoteURL,
		ProjectIDs:   r.ProjectIDs,
		Shape:        domain.ShapeFromComponents(components),
		Components:   nonNil(compSummaries),
		ReviewCount:  len(reviewItems(components, links, environments)),
		LastScan:     lastScan,
		Environments: nonNil(environmentSummaries(environments)),
		GitWarning:   r.GitWarning,
		UpdatedAt:    r.UpdatedAt,
	}
}

func memberRepositories(project domain.InitiativeProject, repos []domain.Repository) []domain.Repository {
	var members []domain.Repository
	for _, r := range repos {
		for _, pid := range r.ProjectIDs {
			if pid == project.ID {
				members = append(members, r)
				break
			}
		}
	}
	return members
}

// buildProjectOverview never touches the store: everything it needs is
// already in data, so ProjectsOverview can build every project's card from
// one shared load.
func buildProjectOverview(project domain.InitiativeProject, data overviewData, projectsByID map[uuid.UUID]domain.InitiativeProject) domain.ProjectOverview {
	members := memberRepositories(project, data.repos)
	memberIDs := make(map[uuid.UUID]bool, len(members))
	for _, r := range members {
		memberIDs[r.ID] = true
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })

	repoSummaries := make([]domain.RepositorySummary, 0, len(members))
	shapes := make([]domain.RepoShape, 0, len(members))
	reviewCount := 0
	for _, r := range members {
		summary := data.summaries[r.ID]
		repoSummaries = append(repoSummaries, summary)
		shapes = append(shapes, summary.Shape)
		reviewCount += summary.ReviewCount
	}

	crossProjectIDs := map[uuid.UUID]bool{}
	crossLinks := 0
	for _, l := range data.allLinks {
		if l.ToComponentID == nil {
			continue
		}
		targetRepo, ok := data.componentRepo[*l.ToComponentID]
		if !ok {
			continue
		}
		sourceIsMember, targetIsMember := memberIDs[l.RepositoryID], memberIDs[targetRepo]
		if sourceIsMember == targetIsMember {
			continue
		}
		crossLinks++
		farRepo := targetRepo
		if !sourceIsMember {
			farRepo = l.RepositoryID
		}
		for _, pid := range data.repoProjects[farRepo] {
			if pid != project.ID {
				crossProjectIDs[pid] = true
			}
		}
	}
	crossProjects := make([]domain.ProjectRef, 0, len(crossProjectIDs))
	for pid := range crossProjectIDs {
		crossProjects = append(crossProjects, domain.ProjectRef{ID: pid, Name: projectsByID[pid].Name})
	}
	sort.Slice(crossProjects, func(i, j int) bool { return crossProjects[i].Name < crossProjects[j].Name })

	sharedResourceIDs := map[uuid.UUID]bool{}
	for resourceID, linkers := range data.resourceRepos {
		linkedByMember, linkedByOutsider := false, false
		for repoID := range linkers {
			if memberIDs[repoID] {
				linkedByMember = true
			} else {
				linkedByOutsider = true
			}
		}
		if linkedByMember && linkedByOutsider {
			sharedResourceIDs[resourceID] = true
		}
	}
	sharedResources := make([]domain.ResourceRefView, 0, len(sharedResourceIDs))
	for id := range sharedResourceIDs {
		if res, ok := data.resourcesByID[id]; ok {
			sharedResources = append(sharedResources, domain.ResourceRefView{ID: res.ID, Kind: res.Kind, Name: res.Name})
		}
	}
	sort.Slice(sharedResources, func(i, j int) bool { return sharedResources[i].Name < sharedResources[j].Name })

	return domain.ProjectOverview{
		ID:              project.ID,
		Name:            project.Name,
		Description:     project.Description,
		Type:            domain.ComputeProjectType(shapes),
		Repositories:    nonNil(repoSummaries),
		ReviewCount:     reviewCount,
		CrossProjects:   nonNil(crossProjects),
		CrossLinks:      crossLinks,
		SharedResources: nonNil(sharedResources),
	}
}

// ProjectsOverview is GET /v1/projects/overview.
func (s *Service) ProjectsOverview(ctx context.Context) (domain.ProjectsOverview, error) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		return domain.ProjectsOverview{}, err
	}
	data, err := s.loadOverviewData(ctx)
	if err != nil {
		return domain.ProjectsOverview{}, err
	}
	projectsByID := make(map[uuid.UUID]domain.InitiativeProject, len(projects))
	for _, p := range projects {
		projectsByID[p.ID] = p
	}

	overviews := make([]domain.ProjectOverview, 0, len(projects))
	for _, p := range projects {
		overviews = append(overviews, buildProjectOverview(p, data, projectsByID))
	}
	sort.Slice(overviews, func(i, j int) bool { return overviews[i].Name < overviews[j].Name })

	var unassigned []domain.RepositorySummary
	for _, r := range data.repos {
		if len(r.ProjectIDs) == 0 {
			unassigned = append(unassigned, data.summaries[r.ID])
		}
	}
	sort.Slice(unassigned, func(i, j int) bool { return unassigned[i].Name < unassigned[j].Name })

	return domain.ProjectsOverview{
		Projects:   nonNil(overviews),
		Unassigned: nonNil(unassigned),
	}, nil
}

// ProjectOverview is GET /v1/projects/:projectId/overview.
func (s *Service) ProjectOverview(ctx context.Context, projectID uuid.UUID) (domain.ProjectDetail, error) {
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	data, err := s.loadOverviewData(ctx)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	projectsByID := make(map[uuid.UUID]domain.InitiativeProject, len(projects))
	for _, p := range projects {
		projectsByID[p.ID] = p
	}

	overview := buildProjectOverview(project, data, projectsByID)

	var components []domain.Component
	var links []domain.ComponentLink
	var environments []domain.ComponentEnvironment
	for _, r := range memberRepositories(project, data.repos) {
		components = append(components, data.componentsByRepo[r.ID]...)
		links = append(links, data.linksByRepo[r.ID]...)
		environments = append(environments, data.environmentsByRepo[r.ID]...)
	}

	return domain.ProjectDetail{
		ProjectOverview: overview,
		Review:          nonNil(reviewItems(components, links, environments)),
	}, nil
}
