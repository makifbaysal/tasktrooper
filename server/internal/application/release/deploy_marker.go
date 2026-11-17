package release

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DeployMarker is told about every deploy that went live (the connected
// telemetry's deploy markers). It is best effort and never fails a release.
type DeployMarker interface {
	MarkDeployed(ctx context.Context, r domain.Release)
}

// SetDeployMarker wires the marker after the telemetry connection is built.
func (s *Service) SetDeployMarker(m DeployMarker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marker = m
}

// markDeployed runs off the sweeper's path: a slow or unreachable telemetry
// must not hold up the release that just went live.
func (s *Service) markDeployed(ctx context.Context, r domain.Release) {
	s.mu.Lock()
	m := s.marker
	s.mu.Unlock()
	if m == nil {
		return
	}
	go m.MarkDeployed(context.WithoutCancel(ctx), r)
}
