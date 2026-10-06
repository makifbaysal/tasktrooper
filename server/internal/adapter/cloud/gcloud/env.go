package gcloud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudEnvManager for cloud_run_service resources,
// against the references checked on 2026-10-05:
//   - https://docs.cloud.google.com/run/docs/reference/rest/v2/projects.locations.services/patch
//     PATCH /v2/{service.name} with the whole Service; it starts a new
//     revision, which is why a write rolls out by itself.
//   - https://docs.cloud.google.com/run/docs/reference/rest/v2/Container
//     env[] is {name, value} or {name, valueSource.secretKeyRef{secret,
//     version}}; secret is the bare secret id for a same-project secret.
//   - https://docs.cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets
//     POST v1/projects/{p}/secrets?secretId=, POST …/{id}:addVersion,
//     GET …/{id}:getIamPolicy, POST …/{id}:setIamPolicy.
//
// A secret is never written as a plain env value: it becomes a Secret Manager
// version, the service reads it through secretKeyRef, and its runtime service
// account is granted roles/secretmanager.secretAccessor on that one secret.
var _ port.CloudEnvManager = (*Provider)(nil)

const secretAccessorRole = "roles/secretmanager.secretAccessor"

func (p *Provider) EnvCapabilities(ref domain.CloudResourceRef) (domain.EnvCapabilities, error) {
	if ref.Kind != domain.CloudResourceCloudRunService {
		return domain.EnvCapabilities{}, port.ErrUnsupported
	}
	return domain.EnvCapabilities{
		Targets:       []domain.DeployEnvironment{domain.EnvironmentProduction},
		WritesRollOut: true,
	}, nil
}

// readService reads the service as a generic document, so a write sends every
// field back exactly as read and drops nothing this package does not model.
func (p *Provider) readService(ctx context.Context, c *Client, ref domain.CloudResourceRef) (map[string]any, error) {
	var svc map[string]any
	if err := c.get(ctx, c.runBaseURL, "/v2/"+ref.ID, &svc); err != nil {
		return nil, classifyRunError(err)
	}
	return svc, nil
}

// ingressContainer is the container that serves requests — the one with a
// port — or the first when none declares one.
func ingressContainer(svc map[string]any) (map[string]any, error) {
	template, _ := svc["template"].(map[string]any)
	containers, _ := template["containers"].([]any)
	if len(containers) == 0 {
		return nil, errors.New("gcloud: the service template has no container")
	}
	for _, raw := range containers {
		if c, ok := raw.(map[string]any); ok {
			if ports, _ := c["ports"].([]any); len(ports) > 0 {
				return c, nil
			}
		}
	}
	first, ok := containers[0].(map[string]any)
	if !ok {
		return nil, errors.New("gcloud: the service template's container is unreadable")
	}
	return first, nil
}

func (p *Provider) ListEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) ([]domain.CloudEnvVar, error) {
	if _, err := p.EnvCapabilities(ref); err != nil {
		return nil, err
	}
	c, err := p.clientFor(cred)
	if err != nil {
		return nil, err
	}
	svc, err := p.readService(ctx, c, ref)
	if err != nil {
		return nil, err
	}
	container, err := ingressContainer(svc)
	if err != nil {
		return nil, err
	}
	env, _ := container["env"].([]any)
	vars := make([]domain.CloudEnvVar, 0, len(env))
	for _, raw := range env {
		e, _ := raw.(map[string]any)
		if name, _ := e["name"].(string); name != "" {
			vars = append(vars, domain.CloudEnvVar{Key: name, Targets: []domain.DeployEnvironment{domain.EnvironmentProduction}})
		}
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Key < vars[j].Key })
	return vars, nil
}

func (p *Provider) UpsertEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	if _, err := p.EnvCapabilities(ref); err != nil {
		return err
	}
	if len(vars) == 0 {
		return nil
	}
	c, err := p.clientFor(cred)
	if err != nil {
		return err
	}
	svc, err := p.readService(ctx, c, ref)
	if err != nil {
		return err
	}
	container, err := ingressContainer(svc)
	if err != nil {
		return err
	}
	project := projectIDFromRef(ref, c)
	template, _ := svc["template"].(map[string]any)

	var runtimeSA string
	env, _ := container["env"].([]any)
	for _, v := range vars {
		entry := map[string]any{"name": v.Key}
		if v.Sensitive {
			if runtimeSA == "" {
				if runtimeSA, err = p.runtimeServiceAccount(ctx, c, project, template); err != nil {
					return err
				}
			}
			id := secretID(ref, v.Key)
			if err := p.storeSecret(ctx, c, project, id, v.Value, runtimeSA); err != nil {
				return err
			}
			entry["valueSource"] = map[string]any{"secretKeyRef": map[string]any{"secret": id, "version": "latest"}}
		} else {
			entry["value"] = v.Value
		}
		env = upsertEnvEntry(env, entry)
	}
	container["env"] = env
	// A named revision must be unique; the old name would make the new
	// revision collide with the one it was read from.
	delete(template, "revision")

	if err := c.patch(ctx, c.runBaseURL, "/v2/"+ref.ID, svc, nil); err != nil {
		return classifyEnvWriteError(err, "run.services.update")
	}
	return nil
}

func upsertEnvEntry(env []any, entry map[string]any) []any {
	for i, raw := range env {
		if e, ok := raw.(map[string]any); ok && e["name"] == entry["name"] {
			env[i] = entry
			return env
		}
	}
	return append(env, entry)
}

var secretIDUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// secretID names the Secret Manager secret holding one variable of one
// service: stable, so a later write adds a version instead of a new secret.
func secretID(ref domain.CloudResourceRef, key string) string {
	service := ref.Name
	if service == "" {
		service = shortName(ref.ID)
	}
	id := secretIDUnsafe.ReplaceAllString("tt-"+service+"-"+key, "-")
	if len(id) > 255 {
		id = id[:255]
	}
	return id
}

func (p *Provider) storeSecret(ctx context.Context, c *Client, project, id, value, runtimeSA string) error {
	base := c.secretManagerBaseURL
	parent := "/v1/projects/" + url.PathEscape(project) + "/secrets"
	create := map[string]any{
		"replication": map[string]any{"automatic": map[string]any{}},
		"labels":      map[string]string{"managed-by": "tasktrooper"},
	}
	err := c.send(ctx, http.MethodPost, base, parent+"?secretId="+url.QueryEscape(id), create, nil)
	var apiErr *apiError
	if err != nil && !(errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict) {
		return classifyEnvWriteError(err, "secretmanager.secrets.create")
	}
	version := map[string]any{"payload": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte(value))}}
	if err := c.send(ctx, http.MethodPost, base, parent+"/"+url.PathEscape(id)+":addVersion", version, nil); err != nil {
		return classifyEnvWriteError(err, "secretmanager.versions.add")
	}
	return p.grantSecretAccess(ctx, c, base, parent+"/"+url.PathEscape(id), runtimeSA)
}

type iamPolicy struct {
	Version  int          `json:"version,omitempty"`
	Etag     string       `json:"etag,omitempty"`
	Bindings []iamBinding `json:"bindings,omitempty"`
}

type iamBinding struct {
	Role      string         `json:"role"`
	Members   []string       `json:"members"`
	Condition map[string]any `json:"condition,omitempty"`
}

func (p *Provider) grantSecretAccess(ctx context.Context, c *Client, base, secretPath, serviceAccount string) error {
	member := "serviceAccount:" + serviceAccount
	var policy iamPolicy
	if err := c.send(ctx, http.MethodGet, base, secretPath+":getIamPolicy", nil, &policy); err != nil {
		return classifyEnvWriteError(err, "secretmanager.secrets.getIamPolicy")
	}
	for i, b := range policy.Bindings {
		if b.Role != secretAccessorRole || b.Condition != nil {
			continue
		}
		for _, m := range b.Members {
			if m == member {
				return nil
			}
		}
		policy.Bindings[i].Members = append(policy.Bindings[i].Members, member)
		return p.setSecretPolicy(ctx, c, base, secretPath, policy)
	}
	policy.Bindings = append(policy.Bindings, iamBinding{Role: secretAccessorRole, Members: []string{member}})
	return p.setSecretPolicy(ctx, c, base, secretPath, policy)
}

func (p *Provider) setSecretPolicy(ctx context.Context, c *Client, base, secretPath string, policy iamPolicy) error {
	if err := c.send(ctx, http.MethodPost, base, secretPath+":setIamPolicy", map[string]any{"policy": policy}, nil); err != nil {
		return classifyEnvWriteError(err, "secretmanager.secrets.setIamPolicy")
	}
	return nil
}

// runtimeServiceAccount is the identity the service's instances read secrets
// as: the template's own, or the project's default compute account when the
// template names none.
func (p *Provider) runtimeServiceAccount(ctx context.Context, c *Client, project string, template map[string]any) (string, error) {
	if sa, _ := template["serviceAccount"].(string); strings.TrimSpace(sa) != "" {
		return strings.TrimSpace(sa), nil
	}
	var proj struct {
		ProjectNumber string `json:"projectNumber"`
	}
	if err := c.send(ctx, http.MethodGet, c.resourceManagerBaseURL, "/v1/projects/"+url.PathEscape(project), nil, &proj); err != nil {
		return "", classifyEnvWriteError(err, "resourcemanager.projects.get")
	}
	if proj.ProjectNumber == "" {
		return "", fmt.Errorf("gcloud: cannot tell %s's default compute service account: no project number", project)
	}
	return proj.ProjectNumber + "-compute@developer.gserviceaccount.com", nil
}

// classifyEnvWriteError names the permission a 403 is missing, the way
// classifyWriteError does for a rollback.
func classifyEnvWriteError(err error, permission string) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return fmt.Errorf("gcloud: %v: %w: the service account needs %s", apiErr, port.ErrCloudWriteDenied, permission)
	}
	return classifyRunError(err)
}

// Redeploy is not needed on Cloud Run: every write above already starts a new
// revision.
func (p *Provider) Redeploy(context.Context, domain.CloudCredential, domain.CloudResourceRef) (domain.CloudDeployment, error) {
	return domain.CloudDeployment{}, port.ErrUnsupported
}
