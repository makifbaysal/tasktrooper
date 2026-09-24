package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LegacyCloudSource reads the tables cloud.Service.Boot replaces, once at
// startup, so an existing Vercel/Google Cloud connection and its per-repository
// bindings carry forward as cloud_accounts / component_environments rows
// instead of asking the operator to reconnect and re-bind everything. The
// bool return on the two credential reads distinguishes "nothing was ever
// connected" from a connected-but-empty value.
type LegacyCloudSource interface {
	LegacyVercelCredential(ctx context.Context) (domain.LegacyVercelCredential, bool, error)
	LegacyGCloudCredential(ctx context.Context) (domain.LegacyGCloudCredential, bool, error)
	ListLegacyVercelProjectLinks(ctx context.Context) ([]domain.LegacyVercelProjectLink, error)
	ListLegacyVercelHostingLinks(ctx context.Context) ([]domain.LegacyHostingLink, error)
	ListLegacyGCloudResourceBindings(ctx context.Context) ([]domain.LegacyGCloudResourceBinding, error)
}
