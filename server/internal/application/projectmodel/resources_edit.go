package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxResourceNameLen = 120

// ListWorkspaceResources is GET /v1/resources: one row per system resource
// with at least one non-dismissed link, so the human can spot the "Database"
// and "PostgreSQL" resources a scanner minted for the same instance and merge
// them.
func (s *Service) ListWorkspaceResources(ctx context.Context) ([]domain.WorkspaceResource, error) {
	data, err := s.loadOverviewData(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspace resources: %w", err)
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspace resources: %w", err)
	}
	projectsByID := make(map[uuid.UUID]domain.InitiativeProject, len(projects))
	for _, p := range projects {
		projectsByID[p.ID] = p
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

	linkCount := map[uuid.UUID]int{}
	userComponents := map[uuid.UUID]map[uuid.UUID]bool{}
	seenResource := map[uuid.UUID]bool{}
	var order []uuid.UUID
	for _, l := range data.allLinks {
		if l.ToResourceID == nil || l.Status == domain.LinkDismissed {
			continue
		}
		rid := *l.ToResourceID
		if !seenResource[rid] {
			seenResource[rid] = true
			order = append(order, rid)
		}
		linkCount[rid]++
		if userComponents[rid] == nil {
			userComponents[rid] = map[uuid.UUID]bool{}
		}
		userComponents[rid][l.FromComponentID] = true
	}

	out := make([]domain.WorkspaceResource, 0, len(order))
	for _, rid := range order {
		res, ok := data.resourcesByID[rid]
		if !ok {
			continue
		}

		var users []domain.ResourceUser
		for compID := range userComponents[rid] {
			comp, ok := componentByID[compID]
			if !ok {
				continue
			}
			repo, ok := repoByID[comp.RepositoryID]
			if !ok {
				continue
			}
			users = append(users, domain.ResourceUser{
				RepositoryID:   repo.ID,
				RepositoryName: repo.Name,
				ProjectIDs:     nonNil(repo.ProjectIDs),
				ComponentID:    comp.ID,
				ComponentPath:  comp.Path,
			})
		}
		sort.Slice(users, func(i, j int) bool {
			if users[i].RepositoryName != users[j].RepositoryName {
				return users[i].RepositoryName < users[j].RepositoryName
			}
			return users[i].ComponentPath < users[j].ComponentPath
		})

		projSet := map[uuid.UUID]bool{}
		for _, u := range users {
			for _, pid := range u.ProjectIDs {
				projSet[pid] = true
			}
		}
		var projRefs []domain.ProjectRef
		for pid := range projSet {
			p, ok := projectsByID[pid]
			if !ok {
				continue
			}
			projRefs = append(projRefs, domain.ProjectRef{ID: p.ID, Name: p.Name})
		}
		sort.Slice(projRefs, func(i, j int) bool { return projRefs[i].Name < projRefs[j].Name })

		out = append(out, domain.WorkspaceResource{
			Resource:  res,
			Users:     nonNil(users),
			Projects:  nonNil(projRefs),
			LinkCount: linkCount[rid],
		})
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Resource, out[j].Resource
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.ID.String() < b.ID.String()
	})
	return nonNil(out), nil
}

func validateResourceName(patch domain.ResourcePatch) (string, error) {
	if patch.Name == nil {
		return "", fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	name := strings.TrimSpace(*patch.Name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(name) > maxResourceNameLen {
		return "", fmt.Errorf("%w: name must be %d characters or fewer", ErrInvalidInput, maxResourceNameLen)
	}
	return name, nil
}

// RenameResource is PATCH /v1/resources/:resourceId: the new name is locked
// against every future EnsureResource upsert, so a rescan's detected name
// never overwrites a human's correction.
func (s *Service) RenameResource(ctx context.Context, id uuid.UUID, patch domain.ResourcePatch) (domain.SystemResource, error) {
	name, err := validateResourceName(patch)
	if err != nil {
		return domain.SystemResource{}, err
	}
	saved, err := s.store.RenameResource(ctx, id, name)
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("rename resource: %w", err)
	}
	return saved, nil
}

// MergeResources is POST /v1/resources/:resourceId/merge: source's links,
// aliases and identity key all move onto the target, and source is deleted.
// The target's own kind/name win over source's.
func (s *Service) MergeResources(ctx context.Context, sourceID uuid.UUID, req domain.MergeResourceRequest) (domain.SystemResource, error) {
	if sourceID == req.IntoResourceID {
		return domain.SystemResource{}, fmt.Errorf("%w: cannot merge a resource into itself", ErrInvalidInput)
	}
	if _, err := s.store.GetResource(ctx, sourceID); err != nil {
		return domain.SystemResource{}, fmt.Errorf("merge resources: %w", err)
	}
	if _, err := s.store.GetResource(ctx, req.IntoResourceID); err != nil {
		return domain.SystemResource{}, fmt.Errorf("merge resources: %w", err)
	}
	if err := s.store.MergeResources(ctx, sourceID, req.IntoResourceID); err != nil {
		return domain.SystemResource{}, fmt.Errorf("merge resources: %w", err)
	}
	target, err := s.store.GetResource(ctx, req.IntoResourceID)
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("merge resources: %w", err)
	}
	return target, nil
}

// SplitResource is POST /v1/resources/:resourceId/split: the given links move
// off the resource in the URL and onto a freshly minted one with its own
// identity key, so a later merge/split never collides with a scanner signal.
func (s *Service) SplitResource(ctx context.Context, resourceID uuid.UUID, req domain.SplitResourceRequest) (domain.SystemResource, error) {
	if len(req.LinkIDs) == 0 {
		return domain.SystemResource{}, fmt.Errorf("%w: link_ids is required", ErrInvalidInput)
	}
	source, err := s.store.GetResource(ctx, resourceID)
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("split resource: %w", err)
	}

	links := make([]domain.ComponentLink, 0, len(req.LinkIDs))
	for _, id := range req.LinkIDs {
		link, err := s.store.GetLink(ctx, id)
		if err != nil {
			return domain.SystemResource{}, fmt.Errorf("split resource: %w", err)
		}
		if link.ToResourceID == nil || *link.ToResourceID != resourceID {
			return domain.SystemResource{}, fmt.Errorf("%w: link %s does not target this resource", ErrInvalidInput, id)
		}
		links = append(links, link)
	}

	newResource, err := s.store.EnsureResource(ctx, domain.SystemResource{
		Kind:        source.Kind,
		Vendor:      source.Vendor,
		Name:        source.Name,
		IdentityKey: fmt.Sprintf("split:%s", uuid.New()),
		Details:     source.Details,
	})
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("split resource: %w", err)
	}
	if source.NameLocked {
		newResource, err = s.store.RenameResource(ctx, newResource.ID, source.Name)
		if err != nil {
			return domain.SystemResource{}, fmt.Errorf("split resource: %w", err)
		}
	}

	for _, link := range links {
		link.ToResourceID = &newResource.ID
		link.ToComponentID = nil
		link.Status = domain.LinkConfirmed
		link.AutoConfirmed = false
		link.Reason = ""
		if _, err := s.store.SaveLink(ctx, link); err != nil {
			return domain.SystemResource{}, fmt.Errorf("split resource: %w", err)
		}
	}
	return newResource, nil
}
