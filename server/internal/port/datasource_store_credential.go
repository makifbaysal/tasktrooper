package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// StoreCredentialReader is how data sources reach the workspace's store
// credentials (App Store Connect, Google Play; one per provider): a source of
// a kind that reads one stores only the provider, and the kind reads the
// credential when it calls. A provider with no credential saved is
// ErrNotFound.
type StoreCredentialReader interface {
	StoreCredential(ctx context.Context, provider string) (domain.StoreCredential, error)
}
