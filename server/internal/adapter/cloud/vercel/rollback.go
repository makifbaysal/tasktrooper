package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudRollbacker against the routes verified live
// against Vercel's REST API reference (2026-09-25):
//   - rollback: https://vercel.com/docs/rest-api/projects/point-production-traffic-to-a-previous-production-deployment-by-id
//     POST /v1/projects/{projectId}/rollback/{deploymentId}
//   - promote:  https://vercel.com/docs/rest-api/projects/point-production-traffic-to-a-given-deployment
//     POST /v10/projects/{projectId}/promote/{deploymentId}
//   - current:  https://vercel.com/docs/rest-api/deployments/list-deployments
//     GET /v6/deployments?projectId=&target=production — each deployment's
//     readySubstate (PROMOTED/ROLLING/STAGED) says whether it has actually
//     taken production traffic; target=production alone only says it was
//     BUILT for production (see Current's doc comment).
//
// Per https://vercel.com/docs/instant-rollback#undo-a-rollback, an instant
// rollback turns OFF automatic assignment of production domains to new
// deployments; only promoting a deployment (the same /v10/.../promote route)
// turns it back on. RollbackEnvironment alone is therefore not enough to
// resume normal on_merge delivery — the caller must Promote once the revert
// commit's deployment is ready.
var _ port.CloudRollbacker = (*Provider)(nil)

// postAction calls a Vercel write route that answers 2xx with no body worth
// decoding (rollback/promote): the API's OpenAPI reference documents only
// status codes for these two operations, not a response schema.
func (c *Client) postAction(ctx context.Context, token, path string, query url.Values) error {
	u := c.base() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)
		msg := payload.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	return nil
}

// wrapVercelWriteErr diverges from wrapVercelErr only on 403: on the read
// endpoints a 403 means "the token cannot see this team/project" (folded into
// ErrCloudAuth), but on rollback/promote it means the token can read the
// project yet lacks the write scope to move production traffic.
func wrapVercelWriteErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusForbidden {
		return fmt.Errorf("%w: the Vercel token cannot roll back deployments — give it write scope", port.ErrCloudWriteDenied)
	}
	return wrapVercelErr(err)
}

func rollbackTeamID(cred domain.CloudCredential, ref domain.CloudResourceRef, fallback string) string {
	if id := ref.Extra["team_id"]; id != "" {
		return id
	}
	return fallback
}

func (p *Provider) RollbackTo(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deploymentID) == "" {
		return fmt.Errorf("vercel: deployment id is required")
	}
	teamID = rollbackTeamID(cred, ref, teamID)
	path := "/v1/projects/" + url.PathEscape(ref.ID) + "/rollback/" + url.PathEscape(deploymentID)
	if err := p.client.postAction(ctx, token, path, withTeam(nil, teamID)); err != nil {
		return wrapVercelWriteErr(err)
	}
	return nil
}

func (p *Provider) Promote(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deploymentID) == "" {
		return fmt.Errorf("vercel: deployment id is required")
	}
	teamID = rollbackTeamID(cred, ref, teamID)
	path := "/v10/projects/" + url.PathEscape(ref.ID) + "/promote/" + url.PathEscape(deploymentID)
	if err := p.client.postAction(ctx, token, path, withTeam(nil, teamID)); err != nil {
		return wrapVercelWriteErr(err)
	}
	return nil
}

// currentDeploymentLookback bounds how many of the project's production-
// target deployments Current scans for the one actually PROMOTED — an
// instant rollback or a slow rollout can leave several READY production
// deployments behind the one currently live.
const currentDeploymentLookback = 20

// vercelListedDeployment widens rawDeployment with readySubstate, which
// list-deployments carries but rawDeployment (shared with the rest of this
// package) does not decode: PROMOTED means the deployment has actually taken
// production traffic, vs. STAGED (built for production, never aliased) or
// ROLLING (a gradual rollout still in progress). See
// https://vercel.com/docs/rest-api/deployments/list-deployments.
type vercelListedDeployment struct {
	deploymentWithCreator
	ReadySubstate string `json:"readySubstate"`
}

// Current reads the deployment production actually serves — NOT merely the
// newest one built with target=production, which the old implementation
// used: a project can carry several READY production-target deployments at
// once (an instant rollback pins traffic to an EARLIER one without deleting
// the newer, now-unserved ones), so "newest" and "the one currently live" are
// different deployments. readySubstate=PROMOTED is Vercel's own answer to
// which one that is; falling back to the newest when none carries it (an
// older API response, or a project that has never seen production traffic)
// keeps the old behaviour as a last resort rather than erroring out.
func (p *Provider) Current(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudDeployment, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	teamID = rollbackTeamID(cred, ref, teamID)

	q := withTeam(url.Values{}, teamID)
	q.Set("projectId", ref.ID)
	q.Set("target", domain.VercelTargetProduction)
	q.Set("limit", strconv.Itoa(currentDeploymentLookback))
	var out struct {
		Deployments []vercelListedDeployment `json:"deployments"`
	}
	if err := p.client.getJSON(ctx, token, "/v6/deployments", q, &out); err != nil {
		return domain.CloudDeployment{}, wrapVercelErr(err)
	}
	if len(out.Deployments) == 0 {
		return domain.CloudDeployment{}, fmt.Errorf("vercel: %s has no production deployment: %w", ref.Name, port.ErrNotFound)
	}
	for _, d := range out.Deployments {
		if strings.EqualFold(d.ReadySubstate, "PROMOTED") {
			return d.toCloudDeployment(), nil
		}
	}
	return out.Deployments[0].toCloudDeployment(), nil
}
