package projectmodel

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var repoScopedResourceKinds = map[domain.ResourceKind]bool{
	domain.ResourceDatabase: true,
	domain.ResourceCache:    true,
	domain.ResourceQueue:    true,
	domain.ResourceStorage:  true,
	domain.ResourceSearch:   true,
}

// resourceIdentity computes the (kind, vendor, name, identity key) for a
// detected link that targets a resource. Repository-scoped kinds are
// disambiguated by the first env var the signal touches so two components
// talking to different Postgres instances never collapse into one resource.
func resourceIdentity(repositoryID uuid.UUID, link domain.DetectedLink) (kind domain.ResourceKind, vendor, name, identityKey string) {
	t := link.Target
	kind = t.ResourceKind
	vendor = strings.ToLower(strings.TrimSpace(t.Vendor))
	vendorSlug := vendor
	if vendorSlug == "" {
		vendorSlug = slugify(t.Name)
	}
	name = strings.TrimSpace(t.Name)
	if name == "" {
		name = t.Vendor
	}
	if name == "" {
		name = vendorSlug
	}

	if repoScopedResourceKinds[kind] {
		disambiguator := vendorSlug
		if len(link.EnvVars) > 0 {
			disambiguator = strings.ToLower(strings.TrimSpace(link.EnvVars[0]))
		}
		identityKey = fmt.Sprintf("repo:%s:%s:%s", repositoryID, vendorSlug, disambiguator)
		return
	}
	identityKey = fmt.Sprintf("vendor:%s", vendorSlug)
	return
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ensureResources upserts one SystemResource per distinct identity among
// links, and returns every resource-kind link's (componentPath|signalKey) key
// mapped to the resource id, for reconcile to wire ToResourceID from.
func (s *Service) ensureResources(ctx context.Context, repositoryID uuid.UUID, links []domain.DetectedLink) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(links))
	byIdentity := make(map[string]uuid.UUID, len(links))
	for _, link := range links {
		if link.Target.Kind != domain.LinkTargetResource {
			continue
		}
		kind, vendor, name, identityKey := resourceIdentity(repositoryID, link)
		id, ok := byIdentity[identityKey]
		if !ok {
			var details map[string]string
			if kind == domain.ResourceDatabase {
				details = map[string]string{"engine": vendor}
			}
			res, err := s.store.EnsureResource(ctx, domain.SystemResource{
				Kind:        kind,
				Vendor:      vendor,
				Name:        name,
				IdentityKey: identityKey,
				Details:     details,
			})
			if err != nil {
				return nil, fmt.Errorf("ensure resource %s: %w", identityKey, err)
			}
			id = res.ID
			byIdentity[identityKey] = id
		}
		out[link.ComponentPath+"|"+link.SignalKey] = id
	}
	return out, nil
}
