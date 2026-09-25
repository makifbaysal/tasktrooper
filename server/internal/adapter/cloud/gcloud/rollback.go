package gcloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudRollbacker for cloud_run_service resources
// only, against the Cloud Run Admin API v2
// (https://cloud.google.com/run/docs/reference/rest/v2/projects.locations.services):
// PATCH /v2/{service.name}?updateMask=traffic with a traffic entry of
// TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION (a pinned revision) or
// TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST (the service's latest ready
// revision) — both are documented on the Service.traffic field of that same
// reference. cloud_run_job (and every other kind) has no traffic to move, so
// it answers port.ErrUnsupported.
var _ port.CloudRollbacker = (*Provider)(nil)

const (
	trafficAllocationRevision = "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION"
	trafficAllocationLatest   = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
)

type trafficTarget struct {
	Type     string `json:"type"`
	Revision string `json:"revision,omitempty"`
	Percent  int    `json:"percent"`
}

type patchTrafficRequest struct {
	Traffic []trafficTarget `json:"traffic"`
}

// classifyWriteError distinguishes the credential-cannot-write case from
// every other failure classifyRunError already knows how to map, so
// RollbackTo/Promote can tell the release service exactly which permission is
// missing.
func classifyWriteError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return fmt.Errorf("gcloud: %v: %w: the service account needs run.services.update", apiErr, port.ErrCloudWriteDenied)
	}
	return classifyRunError(err)
}

func (p *Provider) patchTraffic(ctx context.Context, c *Client, ref domain.CloudResourceRef, traffic []trafficTarget) error {
	body := patchTrafficRequest{Traffic: traffic}
	if err := c.patch(ctx, c.runBaseURL, "/v2/"+ref.ID+"?updateMask=traffic", body, nil); err != nil {
		return classifyWriteError(err)
	}
	return nil
}

// currentServiceItem re-reads the service — services.patch's traffic block is
// the desired state, not the servable revision list, so this is the smallest
// call that answers "is deploymentID already the latest ready revision".
func (p *Provider) currentServiceItem(ctx context.Context, c *Client, ref domain.CloudResourceRef) (runServiceItem, error) {
	var item runServiceItem
	if err := c.get(ctx, c.runBaseURL, "/v2/"+ref.ID, &item); err != nil {
		return runServiceItem{}, classifyRunError(err)
	}
	return item, nil
}

func (p *Provider) RollbackTo(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	if ref.Kind != domain.CloudResourceCloudRunService {
		return port.ErrUnsupported
	}
	if strings.TrimSpace(deploymentID) == "" {
		return fmt.Errorf("gcloud: revision is required")
	}
	c, err := p.clientFor(cred)
	if err != nil {
		return err
	}
	return p.patchTraffic(ctx, c, ref, []trafficTarget{{Type: trafficAllocationRevision, Revision: deploymentID, Percent: 100}})
}

// Promote puts deploymentID into production. When it is already the
// service's latest ready revision it allocates traffic to
// TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST rather than pinning the revision by
// name, so a later `gcloud run deploy` goes live on its own the way it did
// before any rollback — the Cloud Run analogue of what re-enabling Vercel's
// automatic production-domain assignment does.
func (p *Provider) Promote(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	if ref.Kind != domain.CloudResourceCloudRunService {
		return port.ErrUnsupported
	}
	if strings.TrimSpace(deploymentID) == "" {
		return fmt.Errorf("gcloud: revision is required")
	}
	c, err := p.clientFor(cred)
	if err != nil {
		return err
	}
	item, err := p.currentServiceItem(ctx, c, ref)
	if err != nil {
		return err
	}
	if shortName(item.LatestReadyRevision) == shortName(deploymentID) {
		return p.patchTraffic(ctx, c, ref, []trafficTarget{{Type: trafficAllocationLatest, Percent: 100}})
	}
	return p.patchTraffic(ctx, c, ref, []trafficTarget{{Type: trafficAllocationRevision, Revision: deploymentID, Percent: 100}})
}

// largestTrafficRevision is the revision serving the largest share per
// services.get's trafficStatuses; an entry allocated by
// TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST carries no revision name of its own,
// so that case falls back to the service's latestReadyRevision.
func largestTrafficRevision(item runServiceItem) string {
	revision := ""
	bestPercent := -1
	for _, t := range item.TrafficStatuses {
		if t.Percent > bestPercent {
			bestPercent = t.Percent
			revision = t.Revision
		}
	}
	if revision == "" {
		revision = item.LatestReadyRevision
	}
	return revision
}

func (p *Provider) Current(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudDeployment, error) {
	if ref.Kind != domain.CloudResourceCloudRunService {
		return domain.CloudDeployment{}, port.ErrUnsupported
	}
	c, err := p.clientFor(cred)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	item, err := p.currentServiceItem(ctx, c, ref)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	revision := shortName(largestTrafficRevision(item))
	if revision == "" {
		return domain.CloudDeployment{}, fmt.Errorf("gcloud: %s is serving no revision", ref.Name)
	}

	projectID := projectIDFromRef(ref, c)
	revisions, err := serviceRevisions(ctx, c, ref, 20)
	if err != nil {
		return domain.CloudDeployment{}, classifyRunError(err)
	}
	for _, r := range revisions {
		if shortName(r.Name) == revision {
			return mapRevisionToDeployment(r, ref, projectID), nil
		}
	}
	// The serving revision fell out of the last 20 — still report its
	// identity rather than failing the rollback decision on it.
	return domain.CloudDeployment{ID: revision, Environment: domain.EnvironmentProduction}, nil
}
