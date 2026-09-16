package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SettingsStore interface {
	Get(ctx context.Context) (domain.AppSettings, error)
	Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error)
	// UpdateAnalizAssignment writes the backend/frontend/mobile analiz
	// assignment keys. Each argument follows Update's own leave-alone ("")
	// / reset ("-") / overwrite contract.
	UpdateAnalizAssignment(ctx context.Context, backend, frontend, mobile string) (domain.AppSettings, error)
}

// GitHubTokenStore, GitHub erişim token'ını (PAT / App token) saklar.
type GitHubTokenStore interface {
	GitHubToken(ctx context.Context) (string, error)
	SetGitHubToken(ctx context.Context, token string) error
	DeleteGitHubToken(ctx context.Context) error
}
