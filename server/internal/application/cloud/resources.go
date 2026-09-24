package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const resourceCacheTTL = 60 * time.Second

// ListResources returns one account's resource listing, cached for
// resourceCacheTTL so opening the resource picker repeatedly (or matching
// several signals against the same account) does not hit the provider API
// every time; refresh forces a live re-fetch.
func (s *Service) ListResources(ctx context.Context, accountID uuid.UUID, refresh bool) ([]domain.CloudResource, error) {
	if !refresh {
		if resources, ok := s.cachedResources(accountID); ok {
			return resources, nil
		}
	}

	account, err := s.accounts.GetCloudAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providerFor(account.Provider)
	if !ok {
		return nil, fmt.Errorf("no adapter configured for provider %q: %w", account.Provider, ErrInvalidInput)
	}
	cred, err := s.accounts.CloudCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resources, err := provider.ListResources(ctx, cred)
	if err != nil {
		return nil, err
	}

	s.resourceCacheMu.Lock()
	s.resourceCache[accountID] = resourceCacheEntry{resources: resources, expires: s.now().Add(resourceCacheTTL)}
	s.resourceCacheMu.Unlock()
	return resources, nil
}

func (s *Service) cachedResources(accountID uuid.UUID) ([]domain.CloudResource, bool) {
	s.resourceCacheMu.Lock()
	defer s.resourceCacheMu.Unlock()
	entry, ok := s.resourceCache[accountID]
	if !ok || s.now().After(entry.expires) {
		return nil, false
	}
	return entry.resources, true
}

func (s *Service) forgetResourceCache(accountID uuid.UUID) {
	s.resourceCacheMu.Lock()
	delete(s.resourceCache, accountID)
	s.resourceCacheMu.Unlock()
}

// allResources is every OK account of provider's resource listing, for the
// scan matcher: one account's listing error is logged and skipped rather than
// failing the whole match.
func (s *Service) allResources(ctx context.Context, provider domain.CloudProviderKind) []domain.CloudResource {
	accounts, err := s.accounts.ListCloudAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: listing accounts for resource match failed")
		return nil
	}
	var out []domain.CloudResource
	for _, a := range accounts {
		if a.Provider != provider || a.Status != domain.CloudAccountOK {
			continue
		}
		resources, err := s.ListResources(ctx, a.ID, false)
		if err != nil {
			log.Warn().Err(err).Str("account_id", a.ID.String()).Msg("cloud: listing resources for match failed")
			continue
		}
		out = append(out, resources...)
	}
	return out
}
