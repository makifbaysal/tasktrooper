package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RepoScanner reads one checkout of a repository into a domain.ScanResult and
// reports each stage it passes through emit. root is the checkout on this
// machine. A scanner that reaches the checkout some other way finds the
// repository it is scanning with domain.ScanRepositoryFrom.
type RepoScanner interface {
	Scan(ctx context.Context, root string, emit func(domain.ScanEvent)) (domain.ScanResult, error)
}
