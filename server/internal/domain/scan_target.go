package domain

import "context"

type scanRepositoryKey struct{}

// WithScanRepository names the repository a scan on ctx reads. The scanner is
// handed the checkout's root on this machine; one whose checkout lives
// elsewhere needs the repository itself to find it.
func WithScanRepository(ctx context.Context, repo Repository) context.Context {
	return context.WithValue(ctx, scanRepositoryKey{}, repo)
}

// ScanRepositoryFrom is the repository WithScanRepository put on ctx.
func ScanRepositoryFrom(ctx context.Context) (Repository, bool) {
	repo, ok := ctx.Value(scanRepositoryKey{}).(Repository)
	return repo, ok
}
