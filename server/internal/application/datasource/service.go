// Package datasource is the vendor-neutral observability source model: a
// registry of data source kinds (each a port.DataSourceKindAdapter declaring
// a config schema and capabilities), the configured sources themselves, and
// the signal and recipe catalog that maps vendor-neutral signals onto a
// kind's native queries.
package datasource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// URLPolicySetter must be implemented by every kind adapter that dials a URL
// from the source's config; the service hands it its policy, so one value
// decides where every kind may connect.
type URLPolicySetter interface {
	SetURLPolicy(urlguard.Policy)
}

// CloudAccountsSetter must be implemented by every kind whose schema has a
// cloud_account field: the service hands it the same reader it checks those
// fields against.
type CloudAccountsSetter interface {
	SetCloudAccounts(port.CloudAccountReader)
}

// StoreCredentialsSetter must be implemented by every kind whose schema has a
// store_credential field: the service hands it the same reader it checks
// those fields against.
type StoreCredentialsSetter interface {
	SetStoreCredentials(port.StoreCredentialReader)
}

type Deps struct {
	Store port.DataSourceStore
	// Recipes is optional; without it only the embedded catalog is listed.
	Recipes port.ObservabilityRecipeStore
	// CloudAccounts is optional; without it a cloud_account field accepts no
	// value (not_permitted).
	CloudAccounts port.CloudAccountReader
	// CloudAccountList and CloudLinks are optional; with both, a connected
	// cloud account gets its data source on its own (see cloud_sources.go).
	CloudAccountList port.CloudAccountLister
	CloudLinks       port.CloudSourceLinkStore
	// StoreCredentials is optional; without it a store_credential field
	// accepts no value (not_permitted).
	StoreCredentials port.StoreCredentialReader
	Kinds            []port.DataSourceKindAdapter
	// URLPolicy is required: where a source URL may point is a deployment
	// decision (urlguard.HumanConfigured on a person's own computer, a
	// public-only policy on a shared host), never a default.
	URLPolicy *urlguard.Policy
	// Catalog defaults to the embedded catalog.ObservabilityFS().
	Catalog fs.FS
}

type Service struct {
	store       port.DataSourceStore
	recipeStore port.ObservabilityRecipeStore
	clouds      port.CloudAccountReader
	cloudList   port.CloudAccountLister
	cloudLinks  port.CloudSourceLinkStore
	storeCreds  port.StoreCredentialReader
	kinds       map[domain.DataSourceKind]port.DataSourceKindAdapter
	order       []domain.DataSourceKind
	catalog     *Catalog
	policy      urlguard.Policy
	now         func() time.Time

	mu        sync.Mutex
	observers []func(id uuid.UUID)
	// provisionMu serialises the sources made for cloud accounts, so a sync
	// and a connect never make the same one twice.
	provisionMu sync.Mutex
}

// NewService fails without a URL policy, on a duplicate kind, or on a catalog
// that does not validate — wiring and build defects, not runtime conditions.
func NewService(d Deps) (*Service, error) {
	if d.URLPolicy == nil {
		return nil, errors.New("data sources: a URL policy is required")
	}
	s := &Service{
		store:       d.Store,
		recipeStore: d.Recipes,
		clouds:      d.CloudAccounts,
		cloudList:   d.CloudAccountList,
		cloudLinks:  d.CloudLinks,
		storeCreds:  d.StoreCredentials,
		kinds:       map[domain.DataSourceKind]port.DataSourceKindAdapter{},
		now:         func() time.Time { return time.Now().UTC() },
	}
	for _, k := range d.Kinds {
		if _, dup := s.kinds[k.Kind()]; dup {
			return nil, fmt.Errorf("data source kind %q registered twice", k.Kind())
		}
		s.kinds[k.Kind()] = k
		s.order = append(s.order, k.Kind())
		if setter, ok := k.(CloudAccountsSetter); ok && d.CloudAccounts != nil {
			setter.SetCloudAccounts(d.CloudAccounts)
		}
		if setter, ok := k.(StoreCredentialsSetter); ok && d.StoreCredentials != nil {
			setter.SetStoreCredentials(d.StoreCredentials)
		}
	}
	fsys := d.Catalog
	if fsys == nil {
		fsys = catalog.ObservabilityFS()
	}
	c, err := LoadCatalog(fsys, s.order)
	if err != nil {
		return nil, fmt.Errorf("observability catalog: %w", err)
	}
	s.catalog = c
	s.SetURLPolicy(*d.URLPolicy)
	return s, nil
}

// SetURLPolicy decides where a source URL may point: checked when a source
// is saved, and handed to every kind adapter that dials, which checks it
// again at connect time. NewService calls it with Deps.URLPolicy; call it
// again only before serving.
func (s *Service) SetURLPolicy(p urlguard.Policy) {
	s.policy = p
	for _, k := range s.kinds {
		if setter, ok := k.(URLPolicySetter); ok {
			setter.SetURLPolicy(p)
		}
	}
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

// OnChange registers fn to run, with the source's id, once a source is saved
// again or deleted: whatever a caller derived from it (a cached read) is
// stale from then on.
func (s *Service) OnChange(fn func(id uuid.UUID)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observers = append(s.observers, fn)
}

func (s *Service) changed(id uuid.UUID) {
	s.mu.Lock()
	observers := slices.Clone(s.observers)
	s.mu.Unlock()
	for _, fn := range observers {
		fn(id)
	}
}

func (s *Service) Kinds() []domain.DataSourceKindInfo {
	out := make([]domain.DataSourceKindInfo, 0, len(s.order))
	for _, kind := range s.order {
		adapter := s.kinds[kind]
		schema := adapter.Schema()
		out = append(out, domain.DataSourceKindInfo{
			Kind:         kind,
			Label:        schema.Label,
			Description:  schema.Description,
			Capabilities: nonNilSlice(adapter.Capabilities()),
			Fields:       presentFields(schema.Fields),
		})
	}
	return out
}

func presentFields(fields []domain.DataSourceField) []domain.DataSourceField {
	out := make([]domain.DataSourceField, 0, len(fields))
	for _, f := range fields {
		f.Options = nonNilSlice(f.Options)
		if f.ShowWhen == nil {
			f.ShowWhen = map[string]string{}
		}
		f.Secret = f.IsSecret()
		out = append(out, f)
	}
	return out
}

func (s *Service) Signals() []domain.Signal { return s.catalog.Signals() }

// Recipes is the embedded catalog merged with the recipes a person added; a
// user recipe with a catalog recipe's id replaces it, and one that no longer
// validates (a signal or kind it named is gone) is left out with a warning.
func (s *Service) Recipes(ctx context.Context) ([]domain.Recipe, error) {
	byID := map[string]domain.Recipe{}
	for _, r := range s.catalog.Recipes() {
		byID[r.ID] = r
	}
	if s.recipeStore != nil {
		user, err := s.recipeStore.ListUserRecipes(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range user {
			r.Origin = domain.RecipeOriginUser
			if err := s.catalog.ValidateRecipe(r); err != nil {
				log.Warn().Err(err).Str("recipe", r.ID).Msg("data sources: skipping an invalid user recipe")
				continue
			}
			byID[r.ID] = r
		}
	}
	ids := slices.Sorted(maps.Keys(byID))
	out := make([]domain.Recipe, 0, len(ids))
	for _, id := range ids {
		r := byID[id]
		r.Params = nonNilSlice(r.Params)
		if r.AppliesTo.ResourceParams == nil {
			r.AppliesTo.ResourceParams = map[string]string{}
		}
		if r.AppliesTo.StoreParams == nil {
			r.AppliesTo.StoreParams = map[string]string{}
		}
		out = append(out, r)
	}
	return out, nil
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
