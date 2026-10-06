package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudEnvManager against the routes checked against
// Vercel's REST API reference on 2026-10-05:
//   - list:     https://vercel.com/docs/rest-api/projects/retrieve-the-environment-variables-of-a-project-by-id-or-name
//     GET /v10/projects/{idOrName}/env — answers {envs: [...]}; `target` is an
//     array or a single string; hiddenProductionEnvCount counts production
//     variables the token may not see.
//   - upsert:   https://vercel.com/docs/rest-api/projects/create-one-or-more-environment-variables
//     POST /v10/projects/{idOrName}/env?upsert=true with an array body;
//     answers 201 {created, failed: [{error: {code, message, key}}]}.
//   - redeploy: https://vercel.com/docs/rest-api/deployments/create-a-new-deployment
//     POST /v13/deployments with deploymentId (inherits the project and its
//     git source), target=production and withLatestCommit.
var _ port.CloudEnvManager = (*Provider)(nil)

func (p *Provider) EnvCapabilities(ref domain.CloudResourceRef) (domain.EnvCapabilities, error) {
	if ref.Kind != "" && ref.Kind != domain.CloudResourceVercelProject {
		return domain.EnvCapabilities{}, port.ErrUnsupported
	}
	return domain.EnvCapabilities{Targets: domain.EnvTargets}, nil
}

type rawEnvVar struct {
	Key       string          `json:"key"`
	Target    json.RawMessage `json:"target"`
	GitBranch string          `json:"gitBranch"`
}

func (r rawEnvVar) targets() []domain.DeployEnvironment {
	var many []string
	if err := json.Unmarshal(r.Target, &many); err != nil {
		var one string
		if err := json.Unmarshal(r.Target, &one); err != nil || one == "" {
			return nil
		}
		many = []string{one}
	}
	out := make([]domain.DeployEnvironment, 0, len(many))
	for _, t := range many {
		out = append(out, domain.DeployEnvironment(strings.ToLower(strings.TrimSpace(t))))
	}
	return out
}

func (p *Provider) ListEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) ([]domain.CloudEnvVar, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return nil, err
	}
	teamID = rollbackTeamID(cred, ref, teamID)
	var out struct {
		Envs                     []rawEnvVar `json:"envs"`
		HiddenProductionEnvCount int         `json:"hiddenProductionEnvCount"`
	}
	if err := p.client.getJSON(ctx, token, "/v10/projects/"+url.PathEscape(ref.ID)+"/env", withTeam(nil, teamID), &out); err != nil {
		return nil, wrapVercelErr(err)
	}
	// A hidden variable may be exactly the one the check is looking for, so
	// "not listed" would no longer mean "not set".
	if out.HiddenProductionEnvCount > 0 {
		return nil, fmt.Errorf("vercel: the token cannot see %d of %s's production environment variables — give it access to them",
			out.HiddenProductionEnvCount, ref.Name)
	}
	byKey := map[string]*domain.CloudEnvVar{}
	for _, e := range out.Envs {
		// A branch-scoped preview variable is set for that one branch, not
		// for previews in general.
		if e.Key == "" || e.GitBranch != "" {
			continue
		}
		v, ok := byKey[e.Key]
		if !ok {
			v = &domain.CloudEnvVar{Key: e.Key}
			byKey[e.Key] = v
		}
		for _, t := range e.targets() {
			if !v.SetFor(t) {
				v.Targets = append(v.Targets, t)
			}
		}
	}
	vars := make([]domain.CloudEnvVar, 0, len(byKey))
	for _, v := range byKey {
		vars = append(vars, *v)
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Key < vars[j].Key })
	return vars, nil
}

type envWriteBody struct {
	Key    string   `json:"key"`
	Value  string   `json:"value"`
	Type   string   `json:"type"`
	Target []string `json:"target"`
}

func (p *Provider) UpsertEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	if len(vars) == 0 {
		return nil
	}
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return err
	}
	teamID = rollbackTeamID(cred, ref, teamID)
	body := make([]envWriteBody, 0, len(vars))
	for _, v := range vars {
		typ := "encrypted"
		if v.Sensitive {
			typ = "sensitive"
		}
		targets := make([]string, 0, len(v.Targets))
		for _, t := range v.Targets {
			targets = append(targets, string(t))
		}
		body = append(body, envWriteBody{Key: v.Key, Value: v.Value, Type: typ, Target: targets})
	}
	q := withTeam(url.Values{}, teamID)
	q.Set("upsert", "true")
	var out struct {
		Failed []struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Key     string `json:"key"`
			} `json:"error"`
		} `json:"failed"`
	}
	if err := p.client.sendJSON(ctx, http.MethodPost, token, "/v10/projects/"+url.PathEscape(ref.ID)+"/env", q, body, &out); err != nil {
		return wrapVercelEnvWriteErr(err)
	}
	if len(out.Failed) > 0 {
		reasons := make([]string, 0, len(out.Failed))
		for _, f := range out.Failed {
			reasons = append(reasons, strings.TrimSpace(f.Error.Key+": "+f.Error.Message))
		}
		return fmt.Errorf("vercel: %d environment variable(s) were not saved: %s", len(out.Failed), strings.Join(reasons, "; "))
	}
	return nil
}

func (p *Provider) Redeploy(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudDeployment, error) {
	current, err := p.Current(ctx, cred, ref)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	teamID = rollbackTeamID(cred, ref, teamID)
	name := ref.Name
	if name == "" {
		name = ref.ID
	}
	body := map[string]any{
		"name":             name,
		"project":          ref.ID,
		"deploymentId":     current.ID,
		"target":           domain.VercelTargetProduction,
		"withLatestCommit": true,
	}
	var out struct {
		ID         string  `json:"id"`
		URL        string  `json:"url"`
		ReadyState string  `json:"readyState"`
		CreatedAt  int64   `json:"createdAt"`
		Inspector  *string `json:"inspectorUrl"`
	}
	if err := p.client.sendJSON(ctx, http.MethodPost, token, "/v13/deployments", withTeam(nil, teamID), body, &out); err != nil {
		return domain.CloudDeployment{}, wrapVercelEnvWriteErr(err)
	}
	d := domain.CloudDeployment{
		ID:          out.ID,
		Status:      mapDeploymentStatus(out.ReadyState, ""),
		Environment: domain.EnvironmentProduction,
		CreatedAt:   msTime(out.CreatedAt),
	}
	if out.URL != "" {
		d.URL = httpsOf(out.URL)
	}
	if out.Inspector != nil {
		d.InspectURL = *out.Inspector
	}
	return d, nil
}

// wrapVercelEnvWriteErr reads a 403 the way wrapVercelWriteErr does for
// rollback: the token can see the project but may not change it.
func wrapVercelEnvWriteErr(err error) error {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusForbidden {
		return fmt.Errorf("%w: the Vercel token cannot change this project — give it write access", port.ErrCloudWriteDenied)
	}
	return wrapVercelErr(err)
}

// sendJSON is getJSON for a write with a JSON body. The body may carry secret
// values, so nothing of it is ever echoed into an error.
func (c *Client) sendJSON(ctx context.Context, method, token, path string, query url.Values, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("vercel api: encode %s: %w", path, err)
	}
	u := c.base() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
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
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)
		msg := payload.Error.Message
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("vercel api: decode %s: %w", path, err)
		}
	}
	return nil
}
