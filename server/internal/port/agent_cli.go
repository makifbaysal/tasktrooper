package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AgentCLIStore persists which local agent CLIs this install has connected.
type AgentCLIStore interface {
	Get(ctx context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIConnection, bool, error)
	List(ctx context.Context) ([]domain.AgentCLIConnection, error)
	Set(ctx context.Context, conn domain.AgentCLIConnection) error
	// Clear MUST check the flavor in the statement, not in the caller, so a
	// racing reconnect of the same flavor cannot be deleted.
	Clear(ctx context.Context, flavor domain.AgentCLIFlavor) error
	// DefaultModel is the model an agent set to "default model" runs on for
	// this CLI; empty means the CLI picks its own.
	DefaultModel(ctx context.Context, flavor domain.AgentCLIFlavor) (string, error)
	SetDefaultModel(ctx context.Context, flavor domain.AgentCLIFlavor, model string) error
}
