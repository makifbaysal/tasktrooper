package vercel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudProvider over Client. Every call carries the
// credential's teamId when set; a Ref's own Extra["team_id"] (stamped by
// ListResources) overrides it, so a call keyed off a stored CloudResourceRef
// always addresses the team it was listed from even if the account's default
// scope later changes.
type Provider struct {
	client *Client
	now    func() time.Time
}

func NewProvider(c *Client) *Provider {
	return &Provider{client: c, now: time.Now}
}

var _ port.CloudProvider = (*Provider)(nil)

func (p *Provider) Kind() domain.CloudProviderKind { return domain.CloudVercel }

func vercelCredentials(cred domain.CloudCredential) (token, teamID string, err error) {
	token = strings.TrimSpace(cred.Fields["token"])
	if token == "" {
		return "", "", errors.New("vercel: credential is missing the token field")
	}
	return token, strings.TrimSpace(cred.Fields["team_id"]), nil
}

// wrapVercelErr maps a raw *APIError onto the port sentinels the application
// layer branches on, so the adapter is the only place that knows Vercel's
// status codes.
func wrapVercelErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: %s", port.ErrCloudAuth, ae.Message)
		case http.StatusNotFound:
			return fmt.Errorf("vercel: %s: %w", ae.Message, port.ErrNotFound)
		case http.StatusTooManyRequests:
			return fmt.Errorf("vercel: rate limited: %s", ae.Message)
		}
	}
	return err
}

// teamByID — GET /v2/teams/{id}. Separate from Client.Teams (which lists
// every team a token can act in) because Verify needs the one team the
// credential names, not the whole list.
func (c *Client) teamByID(ctx context.Context, token, teamID string) (domain.VercelTeam, error) {
	var out struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := c.getJSON(ctx, token, "/v2/teams/"+url.PathEscape(teamID), nil, &out); err != nil {
		return domain.VercelTeam{}, err
	}
	return domain.VercelTeam{ID: out.ID, Slug: out.Slug, Name: out.Name}, nil
}

func (p *Provider) Verify(ctx context.Context, cred domain.CloudCredential) (map[string]string, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return nil, err
	}
	user, err := p.client.User(ctx, token)
	if err != nil {
		return nil, wrapVercelErr(err)
	}
	meta := map[string]string{"username": user.Username, "email": user.Email}
	if teamID != "" {
		team, err := p.client.teamByID(ctx, token, teamID)
		if err != nil {
			return nil, wrapVercelErr(err)
		}
		meta["team_id"] = team.ID
		meta["team_slug"] = team.Slug
		meta["team_name"] = team.Name
	}
	return meta, nil
}

// listedProject widens rawProject (api.go) with the fields ListResources and
// Resource need beyond what the hosting link feature reads. Embedding keeps
// the git-link/production-URL decoding in one place instead of duplicating
// it.
type listedProject struct {
	rawProject
	NodeVersion string `json:"nodeVersion"`
}

func (r listedProject) toCloudResource(teamID string) domain.CloudResource {
	proj := r.rawProject.toDomain(teamID)

	labels := map[string]string{}
	if proj.Framework != "" {
		labels["framework"] = proj.Framework
	}
	if proj.RootDirectory != "" {
		labels["root_directory"] = proj.RootDirectory
	}
	if r.NodeVersion != "" {
		labels["node_version"] = r.NodeVersion
	}
	if proj.Link != nil {
		if proj.Link.Type != "" {
			labels["git_provider"] = proj.Link.Type
		}
		if slug := proj.Link.Slug(); slug != "" {
			labels["git_repo"] = slug
		}
	}

	var domains []string
	if r.Targets.Production != nil {
		domains = r.Targets.Production.Alias
	}

	extra := map[string]string{}
	if teamID != "" {
		extra["team_id"] = teamID
	}

	return domain.CloudResource{
		Provider: domain.CloudVercel,
		Ref: domain.CloudResourceRef{
			Kind:  domain.CloudResourceVercelProject,
			ID:    r.ID,
			Name:  r.Name,
			Extra: extra,
		},
		URL:     proj.ProductionURL,
		Domains: domains,
		Labels:  labels,
	}
}

func (p *Provider) ListResources(ctx context.Context, cred domain.CloudCredential) ([]domain.CloudResource, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return nil, err
	}
	var all []domain.CloudResource
	var until int64
	for page := 0; page < maxPages; page++ {
		q := withTeam(url.Values{}, teamID)
		q.Set("limit", strconv.Itoa(projectPageLimit))
		if until > 0 {
			q.Set("until", strconv.FormatInt(until, 10))
		}
		var out struct {
			Projects   []listedProject `json:"projects"`
			Pagination struct {
				Next int64 `json:"next"`
			} `json:"pagination"`
		}
		if err := p.client.getJSON(ctx, token, "/v9/projects", q, &out); err != nil {
			return nil, wrapVercelErr(err)
		}
		for _, r := range out.Projects {
			all = append(all, r.toCloudResource(teamID))
		}
		if out.Pagination.Next == 0 || len(out.Projects) < projectPageLimit || out.Pagination.Next == until {
			break
		}
		until = out.Pagination.Next
	}
	return all, nil
}

func (p *Provider) fetchProject(ctx context.Context, token, teamID, id string) (listedProject, error) {
	var r listedProject
	if err := p.client.getJSON(ctx, token, "/v9/projects/"+url.PathEscape(id), withTeam(nil, teamID), &r); err != nil {
		return listedProject{}, err
	}
	return r, nil
}

// mapResourceStatus is the CloudResourceDetail.Status table: coarser than
// mapDeploymentStatus because the UI has no "canceled" resource state, only
// "still don't know".
func mapResourceStatus(readyState string) domain.CloudResourceStatus {
	switch strings.ToUpper(readyState) {
	case "READY":
		return domain.CloudStatusHealthy
	case "BUILDING", "QUEUED", "INITIALIZING":
		return domain.CloudStatusDeploying
	case "ERROR":
		return domain.CloudStatusFailed
	default:
		return domain.CloudStatusUnknown
	}
}

func (p *Provider) Resource(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.CloudResourceDetail{}, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}

	raw, err := p.fetchProject(ctx, token, teamID, ref.ID)
	if err != nil {
		return domain.CloudResourceDetail{}, wrapVercelErr(err)
	}
	resource := raw.toCloudResource(teamID)

	detail := domain.CloudResourceDetail{
		CloudResource: resource,
		Status:        domain.CloudStatusUnknown,
	}

	// cred.Meta is what Verify stored — reusing it here avoids a second round
	// trip to resolve the console owner slug on every details view.
	owner := cred.Meta["team_slug"]
	if owner == "" {
		owner = cred.Meta["username"]
	}
	if owner != "" && resource.Ref.Name != "" {
		detail.ConsoleURL = fmt.Sprintf("https://vercel.com/%s/%s", owner, resource.Ref.Name)
	}

	var facts []domain.KeyValue
	addFact := func(label, value string) {
		if value != "" {
			facts = append(facts, domain.KeyValue{Label: label, Value: value})
		}
	}
	addFact("framework", resource.Labels["framework"])
	addFact("node_version", resource.Labels["node_version"])
	addFact("root_directory", resource.Labels["root_directory"])
	addFact("git_repo", resource.Labels["git_repo"])
	if raw.Link != nil {
		addFact("production_branch", raw.Link.ProductionBranch)
	}
	detail.Facts = facts

	deployments, err := p.listDeployments(ctx, token, teamID, ref.ID, domain.VercelTargetProduction, 1)
	if err != nil {
		return domain.CloudResourceDetail{}, wrapVercelErr(err)
	}
	if len(deployments) > 0 {
		d := deployments[0]
		detail.Status = mapResourceStatus(d.ReadyState)
		detail.Revision = d.UID
		cd := d.toCloudDeployment()
		detail.LatestDeployment = &cd
	}

	return detail, nil
}

// deploymentWithCreator widens rawDeployment (deployments.go) with the
// creator object the port.CloudDeployment.Creator field needs; the existing
// hosting/vercelops path never reads it, so rawDeployment itself stays as-is.
type deploymentWithCreator struct {
	rawDeployment
	Creator *struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"creator"`
}

// mapDeploymentStatus is CloudDeployment.Status: unlike mapResourceStatus it
// has a canceled state of its own, so CANCELED keeps its identity here.
func mapDeploymentStatus(readyState, state string) domain.CloudDeploymentStatus {
	s := readyState
	if s == "" {
		s = state
	}
	switch strings.ToUpper(s) {
	case "READY":
		return domain.CloudDeployReady
	case "BUILDING", "QUEUED", "INITIALIZING":
		return domain.CloudDeployBuilding
	case "ERROR":
		return domain.CloudDeployError
	case "CANCELED":
		return domain.CloudDeployCanceled
	default:
		return domain.CloudDeployUnknown
	}
}

func (d deploymentWithCreator) toCloudDeployment() domain.CloudDeployment {
	cd := domain.CloudDeployment{
		ID:            d.UID,
		Status:        mapDeploymentStatus(d.ReadyState, d.State),
		CommitSHA:     metaString(d.Meta, "githubCommitSha", "gitlabCommitSha", "bitbucketCommitSha"),
		CommitMessage: metaString(d.Meta, "githubCommitMessage", "gitlabCommitMessage", "bitbucketCommitMessage"),
		Branch:        metaString(d.Meta, "githubCommitRef", "gitlabCommitRef", "bitbucketCommitRef"),
	}
	if d.URL != "" {
		cd.URL = httpsOf(d.URL)
	}
	if d.InspectorURL != nil {
		cd.InspectURL = *d.InspectorURL
	}
	target := ""
	if d.Target != nil {
		target = *d.Target
	}
	if target == domain.VercelTargetProduction {
		cd.Environment = domain.EnvironmentProduction
	} else {
		cd.Environment = domain.EnvironmentPreview
	}
	cd.CreatedAt = msTime(d.CreatedAt)
	if cd.CreatedAt.IsZero() {
		cd.CreatedAt = msTime(d.Created)
	}
	if ready := msTime(d.Ready); !ready.IsZero() {
		cd.ReadyAt = &ready
	}
	if d.Creator != nil {
		cd.Creator = d.Creator.Username
		if cd.Creator == "" {
			cd.Creator = d.Creator.Email
		}
	}
	return cd
}

const (
	defaultDeploymentsLimit = 20
	maxDeploymentsLimit     = 100
)

// listDeployments — GET /v6/deployments?projectId=&target=&limit=. Kept
// separate from Client.Deployments (deployments.go, /v7, still used by
// vercelops/hosting) so that surface's behaviour is untouched.
func (p *Provider) listDeployments(ctx context.Context, token, teamID, projectID, target string, limit int) ([]deploymentWithCreator, error) {
	q := withTeam(url.Values{}, teamID)
	q.Set("projectId", projectID)
	q.Set("limit", strconv.Itoa(limit))
	if target != "" {
		q.Set("target", target)
	}
	var out struct {
		Deployments []deploymentWithCreator `json:"deployments"`
	}
	if err := p.client.getJSON(ctx, token, "/v6/deployments", q, &out); err != nil {
		return nil, err
	}
	return out.Deployments, nil
}

func (p *Provider) Deployments(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return nil, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}
	if limit <= 0 {
		limit = defaultDeploymentsLimit
	} else if limit > maxDeploymentsLimit {
		limit = maxDeploymentsLimit
	}

	raw, err := p.listDeployments(ctx, token, teamID, ref.ID, "", limit)
	if err != nil {
		return nil, wrapVercelErr(err)
	}
	out := make([]domain.CloudDeployment, 0, len(raw))
	for _, d := range raw {
		out = append(out, d.toCloudDeployment())
	}
	return out, nil
}

func (p *Provider) Errors(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, since time.Time) ([]domain.RuntimeErrorGroup, error) {
	return nil, port.ErrUnsupported
}
