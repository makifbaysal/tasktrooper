package storeops

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.StoreCredentialReader = (*Service)(nil)

// StoreCredential is one provider's decrypted credential, for the data
// source kinds that read the stores' own metrics with it (Google Play
// vitals, App Store Connect metrics). None saved, or an unknown provider, is
// port.ErrNotFound.
func (s *Service) StoreCredential(ctx context.Context, provider string) (domain.StoreCredential, error) {
	if !validProvider(provider) {
		return domain.StoreCredential{}, fmt.Errorf("storeops: unknown credential provider %q: %w", provider, port.ErrNotFound)
	}
	return s.credential(ctx, provider)
}
