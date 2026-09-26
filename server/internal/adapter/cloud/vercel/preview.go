package vercel

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.CloudPreviewer = (*Provider)(nil)

// previewLookback bounds the branch listing Preview picks from. A task branch
// is pushed a handful of times; the PR head is always among the newest.
const previewLookback = 20

const automationBypassScope = "automation-bypass"

// Preview — GET /v7/deployments?projectId=&branch=&limit=, then
// GET /v13/deployments/{id} for the branch alias, which the list omits.
func (p *Provider) Preview(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, branch, sha string) (domain.CloudDeployment, bool, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.CloudDeployment{}, false, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return domain.CloudDeployment{}, false, nil
	}

	q := withTeam(url.Values{}, teamID)
	q.Set("projectId", ref.ID)
	q.Set("branch", branch)
	q.Set("limit", strconv.Itoa(previewLookback))
	var out struct {
		Deployments []deploymentWithCreator `json:"deployments"`
	}
	if err := p.client.getJSON(ctx, token, "/v7/deployments", q, &out); err != nil {
		return domain.CloudDeployment{}, false, wrapVercelErr(err)
	}

	_, belongs := deploymentTarget(domain.EnvironmentPreview)
	var candidates []domain.CloudDeployment
	for _, d := range out.Deployments {
		if belongs(d.target()) {
			candidates = append(candidates, d.toCloudDeployment())
		}
	}
	if len(candidates) == 0 {
		return domain.CloudDeployment{}, false, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].CreatedAt.After(candidates[j].CreatedAt) })

	picked := candidates[0]
	if sha = strings.TrimSpace(sha); sha != "" {
		for _, c := range candidates {
			if domain.SameCommit(c.CommitSHA, sha) {
				picked = c
				break
			}
		}
	}
	picked.BranchURL = p.branchAlias(ctx, token, teamID, picked.ID)
	return picked, true, nil
}

// branchAlias is the deployment's `-git-<branch>-` alias. Vercel truncates
// and hashes long branch names into it, so it is read, never constructed.
// A failed read leaves it empty: the commit URL still opens the preview.
func (p *Provider) branchAlias(ctx context.Context, token, teamID, deploymentID string) string {
	if deploymentID == "" {
		return ""
	}
	var out struct {
		Alias            []string `json:"alias"`
		AutomaticAliases []string `json:"automaticAliases"`
	}
	if err := p.client.getJSON(ctx, token, "/v13/deployments/"+url.PathEscape(deploymentID), withTeam(nil, teamID), &out); err != nil {
		return ""
	}
	for _, a := range append(out.Alias, out.AutomaticAliases...) {
		if strings.Contains(strings.ToLower(a), "-git-") {
			return httpsOf(a)
		}
	}
	return ""
}

type projectProtection struct {
	SSOProtection *struct {
		DeploymentType string `json:"deploymentType"`
	} `json:"ssoProtection"`
	PasswordProtection *struct {
		DeploymentType string `json:"deploymentType"`
	} `json:"passwordProtection"`
	ProtectionBypass map[string]struct {
		Scope string `json:"scope"`
	} `json:"protectionBypass"`
}

// PreviewAccess — GET /v9/projects/{id}. Every deploymentType Vercel offers
// for either protection covers previews, so presence alone means protected.
func (p *Provider) PreviewAccess(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.PreviewAccess, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.PreviewAccess{}, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}
	var raw projectProtection
	if err := p.client.getJSON(ctx, token, "/v9/projects/"+url.PathEscape(ref.ID), withTeam(nil, teamID), &raw); err != nil {
		return domain.PreviewAccess{}, wrapVercelErr(err)
	}
	return raw.toPreviewAccess(), nil
}

func (r projectProtection) toPreviewAccess() domain.PreviewAccess {
	sso, password := r.SSOProtection != nil, r.PasswordProtection != nil
	access := domain.PreviewAccess{Protected: sso || password, Mode: domain.PreviewAccessNone}
	switch {
	case sso && password:
		access.Mode = domain.PreviewAccessVercelAuthAndPassword
	case sso:
		access.Mode = domain.PreviewAccessVercelAuth
	case password:
		access.Mode = domain.PreviewAccessPassword
	}

	secrets := make([]string, 0, len(r.ProtectionBypass))
	for secret, entry := range r.ProtectionBypass {
		if entry.Scope == automationBypassScope && secret != "" {
			secrets = append(secrets, secret)
		}
	}
	if len(secrets) > 0 {
		sort.Strings(secrets)
		access.BypassConfigured = true
		access.BypassSecret = secrets[0]
	}
	return access
}
