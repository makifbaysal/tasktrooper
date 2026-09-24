package projectmodel

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ProjectMap is GET /v1/projects/:projectId/map: every active component of
// the project's own repositories, every resource they link to, and — only
// where a link actually crosses the project boundary — the foreign component
// on the other end.
func (s *Service) ProjectMap(ctx context.Context, projectID uuid.UUID) (domain.ProjectMap, error) {
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return domain.ProjectMap{}, err
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return domain.ProjectMap{}, err
	}
	projectsByID := make(map[uuid.UUID]domain.InitiativeProject, len(projects))
	for _, p := range projects {
		projectsByID[p.ID] = p
	}

	data, err := s.loadOverviewData(ctx)
	if err != nil {
		return domain.ProjectMap{}, err
	}

	members := memberRepositories(project, data.repos)
	memberRepoSet := make(map[uuid.UUID]bool, len(members))
	for _, r := range members {
		memberRepoSet[r.ID] = true
	}

	repoByID := make(map[uuid.UUID]domain.Repository, len(data.repos))
	for _, r := range data.repos {
		repoByID[r.ID] = r
	}
	componentByID := make(map[uuid.UUID]domain.Component)
	for _, comps := range data.componentsByRepo {
		for _, c := range comps {
			componentByID[c.ID] = c
		}
	}
	prodEnvByComponent := make(map[uuid.UUID]domain.ComponentEnvironment)
	for _, envs := range data.environmentsByRepo {
		for _, e := range envs {
			if e.Environment == domain.EnvironmentProduction {
				prodEnvByComponent[e.ComponentID] = e
			}
		}
	}

	nodes := map[string]domain.MapNode{}
	var nodeOrder []string

	// addComponentNode reports false (and adds nothing) for an inactive
	// component: dismissed components never surface as nodes, own or foreign,
	// and a link dangling from one draws no edge either.
	addComponentNode := func(c domain.Component) bool {
		if c.Status != domain.ComponentStatusActive {
			return false
		}
		id := "c:" + c.ID.String()
		if _, exists := nodes[id]; exists {
			return true
		}
		repo := repoByID[c.RepositoryID]
		node := domain.MapNode{
			ID:             id,
			Kind:           domain.MapNodeComponent,
			Tier:           domain.TierForRole(c.Role.Get()),
			Label:          c.DisplayName(),
			Role:           c.Role.Get(),
			ComponentID:    &c.ID,
			RepositoryID:   &repo.ID,
			RepositoryName: repo.Name,
			Path:           c.Path,
			ProjectIDs:     repo.ProjectIDs,
			Foreign:        !memberRepoSet[c.RepositoryID],
			StackSummary:   c.Stack.Get().Summary(3),
		}
		if env, ok := prodEnvByComponent[c.ID]; ok {
			node.Provider = env.Provider
			if env.Health != nil {
				node.Health = env.Health.Status
				node.ErrorCount24h = env.Health.ErrorCount24h
			}
		}
		nodes[id] = node
		nodeOrder = append(nodeOrder, id)
		return true
	}

	addResourceNode := func(resourceID uuid.UUID) (string, bool) {
		id := "r:" + resourceID.String()
		if _, exists := nodes[id]; exists {
			return id, true
		}
		res, ok := data.resourcesByID[resourceID]
		if !ok {
			return "", false
		}
		nodes[id] = domain.MapNode{
			ID:           id,
			Kind:         domain.MapNodeResource,
			Tier:         domain.TierForResource(res.Kind),
			Label:        res.Name,
			ResourceKind: res.Kind,
			Vendor:       res.Vendor,
			ResourceID:   &res.ID,
			SharedWith:   sharedProjectsForResource(res.ID, project.ID, data, projectsByID),
		}
		nodeOrder = append(nodeOrder, id)
		return id, true
	}

	for _, r := range members {
		for _, c := range data.componentsByRepo[r.ID] {
			addComponentNode(c)
		}
	}

	// resourceEdgeGroup collapses parallel links from one component to one
	// resource (the "Database" + "PostgreSQL" merge case) into a single edge;
	// a component-to-component edge is never collapsed.
	type resourceEdgeGroup struct {
		from, to string
		links    []domain.ComponentLink
	}
	resourceGroups := map[string]*resourceEdgeGroup{}
	var resourceGroupOrder []string

	var edges []domain.MapEdge
	for _, l := range data.allLinks {
		if l.Status == domain.LinkDismissed {
			continue
		}
		fromComp, ok := componentByID[l.FromComponentID]
		if !ok {
			continue
		}
		fromInProject := memberRepoSet[fromComp.RepositoryID]

		var toComp domain.Component
		toInProject := false
		switch {
		case l.ToComponentID != nil:
			c, ok := componentByID[*l.ToComponentID]
			if !ok {
				continue
			}
			toComp = c
			toInProject = memberRepoSet[toComp.RepositoryID]
		case l.ToResourceID != nil:
			// A resource has no repository of its own; a resource-linking edge
			// belongs to this project exactly when its source component does.
			toInProject = fromInProject
		default:
			continue // unresolved: no target node to draw an edge to
		}
		if !fromInProject && !toInProject {
			continue
		}
		if !addComponentNode(fromComp) {
			continue
		}
		fromID := "c:" + fromComp.ID.String()

		if l.ToComponentID != nil {
			if !addComponentNode(toComp) {
				continue
			}
			toID := "c:" + toComp.ID.String()
			crossProject := !fromInProject || !toInProject
			edges = append(edges, domain.MapEdge{
				ID:           "e:" + l.ID.String(),
				LinkID:       l.ID,
				From:         fromID,
				To:           toID,
				Protocol:     l.Protocol,
				Detail:       l.Detail,
				Status:       l.Status,
				Source:       l.Source,
				EnvVars:      nonNil(l.EnvVars),
				CrossProject: crossProject,
			})
			continue
		}

		id, ok := addResourceNode(*l.ToResourceID)
		if !ok {
			continue
		}
		key := fromID + "|" + id
		g, exists := resourceGroups[key]
		if !exists {
			g = &resourceEdgeGroup{from: fromID, to: id}
			resourceGroups[key] = g
			resourceGroupOrder = append(resourceGroupOrder, key)
		}
		g.links = append(g.links, l)
	}

	for _, key := range resourceGroupOrder {
		g := resourceGroups[key]
		rep := g.links[0]
		status := domain.LinkConfirmed
		var envVars []string
		seenEnv := map[string]bool{}
		for _, gl := range g.links {
			if gl.ID.String() < rep.ID.String() {
				rep = gl
			}
			if gl.Status == domain.LinkSuggested {
				status = domain.LinkSuggested
			}
			for _, ev := range gl.EnvVars {
				if !seenEnv[ev] {
					seenEnv[ev] = true
					envVars = append(envVars, ev)
				}
			}
		}
		edges = append(edges, domain.MapEdge{
			ID:       "e:" + rep.ID.String(),
			LinkID:   rep.ID,
			From:     g.from,
			To:       g.to,
			Protocol: rep.Protocol,
			Detail:   rep.Detail,
			Status:   status,
			Source:   rep.Source,
			EnvVars:  nonNil(envVars),
		})
	}

	outNodes := make([]domain.MapNode, 0, len(nodeOrder))
	for _, id := range nodeOrder {
		outNodes = append(outNodes, nodes[id])
	}
	sort.Slice(outNodes, func(i, j int) bool {
		if outNodes[i].Kind != outNodes[j].Kind {
			return outNodes[i].Kind < outNodes[j].Kind
		}
		return outNodes[i].Label < outNodes[j].Label
	})
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	return domain.ProjectMap{
		Project: domain.ProjectRef{ID: project.ID, Name: project.Name},
		Nodes:   nonNil(outNodes),
		Edges:   nonNil(edges),
	}, nil
}

// sharedProjectsForResource is every OTHER project (not excludeProjectID)
// with a repository that links resourceID.
func sharedProjectsForResource(resourceID, excludeProjectID uuid.UUID, data overviewData, projectsByID map[uuid.UUID]domain.InitiativeProject) []domain.ProjectRef {
	projSet := map[uuid.UUID]bool{}
	for repoID := range data.resourceRepos[resourceID] {
		for _, pid := range data.repoProjects[repoID] {
			if pid != excludeProjectID {
				projSet[pid] = true
			}
		}
	}
	out := make([]domain.ProjectRef, 0, len(projSet))
	for pid := range projSet {
		out = append(out, domain.ProjectRef{ID: pid, Name: projectsByID[pid].Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return nonNil(out)
}

// mapEdgeExampleLabel is what a WorkspaceMapEdge.Examples entry names each
// side by: a nested component's own path ("services/api"), or its display
// name when it sits at the repository root.
func mapEdgeExampleLabel(c domain.Component) string {
	if c.Path != "" && c.Path != "." {
		return c.Path
	}
	return c.DisplayName()
}

func containsProtocol(list []domain.LinkProtocol, p domain.LinkProtocol) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// WorkspaceMap is GET /v1/projects/map: every project with its repositories
// and component summaries, aggregated per-project-pair edges for projects
// that actually link (independent projects carry none), and resources linked
// from two or more projects.
func (s *Service) WorkspaceMap(ctx context.Context) (domain.WorkspaceMap, error) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		return domain.WorkspaceMap{}, err
	}
	data, err := s.loadOverviewData(ctx)
	if err != nil {
		return domain.WorkspaceMap{}, err
	}

	toWorkspaceRepo := func(r domain.Repository) domain.WorkspaceMapRepository {
		summary := data.summaries[r.ID]
		return domain.WorkspaceMapRepository{ID: r.ID, Name: r.Name, Shape: summary.Shape, Components: nonNil(summary.Components)}
	}

	wsProjects := make([]domain.WorkspaceMapProject, 0, len(projects))
	for _, p := range projects {
		members := memberRepositories(p, data.repos)
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		repos := make([]domain.WorkspaceMapRepository, 0, len(members))
		shapes := make([]domain.RepoShape, 0, len(members))
		for _, r := range members {
			wr := toWorkspaceRepo(r)
			repos = append(repos, wr)
			shapes = append(shapes, wr.Shape)
		}
		wsProjects = append(wsProjects, domain.WorkspaceMapProject{
			ID: p.ID, Name: p.Name, Type: domain.ComputeProjectType(shapes), Repositories: nonNil(repos),
		})
	}
	sort.Slice(wsProjects, func(i, j int) bool { return wsProjects[i].Name < wsProjects[j].Name })

	var unassigned []domain.WorkspaceMapRepository
	for _, r := range data.repos {
		if len(r.ProjectIDs) == 0 {
			unassigned = append(unassigned, toWorkspaceRepo(r))
		}
	}
	sort.Slice(unassigned, func(i, j int) bool { return unassigned[i].Name < unassigned[j].Name })

	componentByID := make(map[uuid.UUID]domain.Component)
	for _, comps := range data.componentsByRepo {
		for _, c := range comps {
			componentByID[c.ID] = c
		}
	}

	type edgeKey struct{ from, to uuid.UUID }
	edgeAgg := map[edgeKey]*domain.WorkspaceMapEdge{}
	var edgeOrder []edgeKey

	for _, l := range data.allLinks {
		if l.Status == domain.LinkDismissed || l.ToComponentID == nil {
			continue
		}
		fromComp, ok := componentByID[l.FromComponentID]
		if !ok {
			continue
		}
		toComp, ok := componentByID[*l.ToComponentID]
		if !ok {
			continue
		}
		for _, fp := range data.repoProjects[fromComp.RepositoryID] {
			for _, tp := range data.repoProjects[toComp.RepositoryID] {
				if fp == tp {
					continue
				}
				key := edgeKey{fp, tp}
				agg, exists := edgeAgg[key]
				if !exists {
					agg = &domain.WorkspaceMapEdge{FromProjectID: fp, ToProjectID: tp}
					edgeAgg[key] = agg
					edgeOrder = append(edgeOrder, key)
				}
				agg.Links++
				if l.Status == domain.LinkSuggested {
					agg.Suggested++
				}
				if !containsProtocol(agg.Protocols, l.Protocol) {
					agg.Protocols = append(agg.Protocols, l.Protocol)
				}
				example := fmt.Sprintf("%s → %s", mapEdgeExampleLabel(fromComp), mapEdgeExampleLabel(toComp))
				if len(agg.Examples) < 3 && !containsString(agg.Examples, example) {
					agg.Examples = append(agg.Examples, example)
				}
			}
		}
	}

	edges := make([]domain.WorkspaceMapEdge, 0, len(edgeOrder))
	for _, key := range edgeOrder {
		edges = append(edges, *edgeAgg[key])
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromProjectID != edges[j].FromProjectID {
			return edges[i].FromProjectID.String() < edges[j].FromProjectID.String()
		}
		return edges[i].ToProjectID.String() < edges[j].ToProjectID.String()
	})

	sharedResourceProjects := map[uuid.UUID]map[uuid.UUID]bool{}
	for resourceID, repos := range data.resourceRepos {
		projSet := map[uuid.UUID]bool{}
		for repoID := range repos {
			for _, pid := range data.repoProjects[repoID] {
				projSet[pid] = true
			}
		}
		if len(projSet) >= 2 {
			sharedResourceProjects[resourceID] = projSet
		}
	}
	sharedResources := make([]domain.WorkspaceSharedResource, 0, len(sharedResourceProjects))
	for resourceID, projSet := range sharedResourceProjects {
		res, ok := data.resourcesByID[resourceID]
		if !ok {
			continue
		}
		ids := make([]uuid.UUID, 0, len(projSet))
		for pid := range projSet {
			ids = append(ids, pid)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		sharedResources = append(sharedResources, domain.WorkspaceSharedResource{
			Resource:   domain.ResourceRefView{ID: res.ID, Kind: res.Kind, Name: res.Name},
			ProjectIDs: ids,
		})
	}
	sort.Slice(sharedResources, func(i, j int) bool { return sharedResources[i].Resource.Name < sharedResources[j].Resource.Name })

	return domain.WorkspaceMap{
		Projects:        nonNil(wsProjects),
		Unassigned:      nonNil(unassigned),
		Edges:           nonNil(edges),
		SharedResources: nonNil(sharedResources),
	}, nil
}
