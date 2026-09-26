package gcloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider is the port.CloudProvider adapter for Google Cloud, built on top of
// Client. Clients are cached by account id + credential hash so a service's
// minted OAuth token (cached inside Client) survives across calls instead of
// being re-exchanged on every request.
type Provider struct {
	newClient func(serviceAccountJSON string) (*Client, error)

	runBaseURL            string
	loggingBaseURL        string
	errorReportingBaseURL string
	tokenURL              string

	mu      sync.Mutex
	clients map[string]*Client
}

var _ port.CloudProvider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{
		newClient:             newServiceAccountClient,
		runBaseURL:            defaultRunBaseURL,
		loggingBaseURL:        defaultLoggingBaseURL,
		errorReportingBaseURL: defaultErrorReportingBaseURL,
		clients:               make(map[string]*Client),
	}
}

func newServiceAccountClient(serviceAccountJSON string) (*Client, error) {
	return New(domain.GCloudCredential{Data: map[string]string{"service_account_json": serviceAccountJSON}})
}

func (p *Provider) SetRunBaseURL(u string)            { p.runBaseURL = u }
func (p *Provider) SetLoggingBaseURL(u string)        { p.loggingBaseURL = u }
func (p *Provider) SetErrorReportingBaseURL(u string) { p.errorReportingBaseURL = u }
func (p *Provider) SetTokenURL(u string)              { p.tokenURL = u }

func (p *Provider) Kind() domain.CloudProviderKind { return domain.CloudGCP }

// clientFor builds (or reuses) the Client for one credential. The cache key
// folds in a hash of the raw key material, not just the account id, so a
// rotated service account cannot reuse a stale token cached under the old
// secret.
func (p *Provider) clientFor(cred domain.CloudCredential) (*Client, error) {
	raw := strings.TrimSpace(cred.Fields["service_account_json"])
	if raw == "" {
		return nil, fmt.Errorf("gcloud: credential missing service_account_json: %w", port.ErrCloudAuth)
	}
	key := clientCacheKey(cred.AccountID, raw)

	p.mu.Lock()
	if c, ok := p.clients[key]; ok {
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()

	c, err := p.newClient(raw)
	if err != nil {
		return nil, fmt.Errorf("gcloud: %w: %w", err, port.ErrCloudAuth)
	}
	c.SetRunBaseURL(p.runBaseURL)
	if p.tokenURL != "" {
		c.SetTokenURL(p.tokenURL)
	}

	p.mu.Lock()
	p.clients[key] = c
	p.mu.Unlock()
	return c, nil
}

func clientCacheKey(accountID uuid.UUID, secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return accountID.String() + ":" + hex.EncodeToString(sum[:])
}

func (p *Provider) Verify(ctx context.Context, cred domain.CloudCredential) (map[string]string, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return nil, err
	}
	if err := c.ValidateAuth(ctx); err != nil {
		return nil, fmt.Errorf("gcloud: verifying credential: %w: %w", err, port.ErrCloudAuth)
	}
	id := c.Identity()
	return map[string]string{"project_id": id.ProjectID, "client_email": id.ClientEmail}, nil
}

func projectIDFromRef(ref domain.CloudResourceRef, c *Client) string {
	if v := ref.Extra["project_id"]; v != "" {
		return v
	}
	return c.projectID
}

func classifyRunError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrCloudAuth)
		case http.StatusNotFound:
			return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrNotFound)
		}
	}
	return err
}

// runContainer is the one field this integration reads off a Cloud Run
// container spec; both services and jobs nest it differently.
type runContainer struct {
	Image string `json:"image"`
}

// runCondition mirrors the Cloud Run v2 Condition message shared by
// services, revisions, jobs and executions.
type runCondition struct {
	Type               string `json:"type"`
	State              string `json:"state"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

// readyCondition finds the "Ready" entry in a Service or Revision's
// conditions list; terminalCondition is the fallback for shapes that only
// ever populate that single field.
func readyCondition(conditions []runCondition, terminal runCondition) runCondition {
	for _, c := range conditions {
		if c.Type == "Ready" {
			return c
		}
	}
	return terminal
}

// executionReadyCondition looks for "Completed" — the terminal condition
// type Executions use instead of "Ready".
func executionReadyCondition(conditions []runCondition) runCondition {
	for _, c := range conditions {
		if c.Type == "Completed" {
			return c
		}
	}
	if len(conditions) > 0 {
		return conditions[len(conditions)-1]
	}
	return runCondition{}
}

func conditionStatus(state string) domain.CloudResourceStatus {
	switch state {
	case "CONDITION_SUCCEEDED":
		return domain.CloudStatusHealthy
	case "CONDITION_RECONCILING", "CONDITION_PENDING":
		return domain.CloudStatusDeploying
	case "CONDITION_FAILED":
		return domain.CloudStatusFailed
	default:
		return domain.CloudStatusUnknown
	}
}

func deploymentStatus(state string) domain.CloudDeploymentStatus {
	switch state {
	case "CONDITION_SUCCEEDED":
		return domain.CloudDeployReady
	case "CONDITION_RECONCILING", "CONDITION_PENDING":
		return domain.CloudDeployBuilding
	case "CONDITION_FAILED":
		return domain.CloudDeployError
	default:
		return domain.CloudDeployUnknown
	}
}

func mergeLabels(labels map[string]string, image string) map[string]string {
	if len(labels) == 0 && image == "" {
		return nil
	}
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	if image != "" {
		out["image"] = image
	}
	return out
}

func firstImage(containers []runContainer) string {
	if len(containers) == 0 {
		return ""
	}
	return containers[0].Image
}

// parseRunResourceName splits projects/<p>/locations/<l>/<collection>/<name>;
// unlike domain.ParseGCloudResourceName it also accepts "jobs", which that
// legacy parser (built only for the two resource kinds gcloudops binds) does
// not know about.
func parseRunResourceName(name, collection string) (projectID, location, short string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "projects" || parts[2] != "locations" || parts[4] != collection {
		return "", "", "", false
	}
	if parts[1] == "" || parts[3] == "" || parts[5] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[5], true
}

type runServiceItem struct {
	Name                string            `json:"name"`
	URI                 string            `json:"uri"`
	Labels              map[string]string `json:"labels"`
	Ingress             string            `json:"ingress"`
	LatestReadyRevision string            `json:"latestReadyRevision"`
	CreateTime          string            `json:"createTime"`
	UpdateTime          string            `json:"updateTime"`
	Template            struct {
		Containers []runContainer `json:"containers"`
		Scaling    struct {
			MinInstanceCount int `json:"minInstanceCount"`
			MaxInstanceCount int `json:"maxInstanceCount"`
		} `json:"scaling"`
	} `json:"template"`
	TrafficStatuses []struct {
		Revision string `json:"revision"`
		Percent  int    `json:"percent"`
	} `json:"trafficStatuses"`
	Conditions        []runCondition `json:"conditions"`
	TerminalCondition runCondition   `json:"terminalCondition"`
}

type runJobItem struct {
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	Template struct {
		Template struct {
			Containers []runContainer `json:"containers"`
		} `json:"template"`
	} `json:"template"`
}

type runRevisionItem struct {
	Name        string            `json:"name"`
	CreateTime  string            `json:"createTime"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Conditions  []runCondition    `json:"conditions"`
}

type runExecutionItem struct {
	Name           string            `json:"name"`
	CreateTime     string            `json:"createTime"`
	CompletionTime string            `json:"completionTime"`
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations"`
	Conditions     []runCondition    `json:"conditions"`
	SucceededCount int               `json:"succeededCount"`
	FailedCount    int               `json:"failedCount"`
	CancelledCount int               `json:"cancelledCount"`
	Template       struct {
		Containers []runContainer `json:"containers"`
	} `json:"template"`
}

func revisionCommitSHA(labels, annotations map[string]string) string {
	if v := labels["commit-sha"]; v != "" {
		return v
	}
	return annotations["gcb-build-id"]
}

func (p *Provider) ListResources(ctx context.Context, cred domain.CloudCredential) ([]domain.CloudResource, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return nil, err
	}

	services, err := p.listServices(ctx, c, cred.AccountID)
	if err != nil {
		return nil, err
	}
	jobs, err := p.listJobs(ctx, c, cred.AccountID)
	if err != nil {
		return nil, err
	}

	out := make([]domain.CloudResource, 0, len(services)+len(jobs))
	out = append(out, services...)
	out = append(out, jobs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.ID < out[j].Ref.ID })
	return out, nil
}

// listServices tries the aggregated "-" location first — the cheap path when
// the project grants it — and falls back to enumerating locations one at a
// time exactly like Client.ListCloudRunServices does, but keeping the raw
// item (Labels, container image) that method's own return type discards.
func (p *Provider) listServices(ctx context.Context, c *Client, accountID uuid.UUID) ([]domain.CloudResource, error) {
	items, err := runServicesPage(ctx, c, "-")
	if err == nil {
		return buildServiceResources(accountID, items), nil
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) || !wildcardRejected(apiErr.Status) {
		return nil, classifyRunError(err)
	}

	locations, err := c.cloudRunLocations(ctx)
	if err != nil {
		return nil, classifyRunError(err)
	}

	var all []runServiceItem
	failures := 0
	var lastErr error
	for _, loc := range locations {
		found, err := runServicesPage(ctx, c, loc)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		all = append(all, found...)
	}
	if len(locations) > 0 && failures == len(locations) {
		return nil, classifyRunError(lastErr)
	}
	return buildServiceResources(accountID, all), nil
}

// listJobs enumerates locations directly: Cloud Run Jobs has no "-" wildcard
// contract to try first the way Services does.
func (p *Provider) listJobs(ctx context.Context, c *Client, accountID uuid.UUID) ([]domain.CloudResource, error) {
	locations, err := c.cloudRunLocations(ctx)
	if err != nil {
		return nil, classifyRunError(err)
	}

	var all []runJobItem
	failures := 0
	var lastErr error
	for _, loc := range locations {
		found, err := runJobsPage(ctx, c, loc)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		all = append(all, found...)
	}
	if len(locations) > 0 && failures == len(locations) {
		return nil, classifyRunError(lastErr)
	}
	return buildJobResources(accountID, all), nil
}

func runServicesPage(ctx context.Context, c *Client, location string) ([]runServiceItem, error) {
	var out []runServiceItem
	pageToken := ""
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, fmt.Errorf("gcloud: cloud run services in %q: still paging after %d pages, refusing to follow the cursor further", location, maxListPages)
		}
		path := "/v2/projects/" + url.PathEscape(c.projectID) + "/locations/" + url.PathEscape(location) + "/services?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var resp struct {
			Services      []runServiceItem `json:"services"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Services...)
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

func runJobsPage(ctx context.Context, c *Client, location string) ([]runJobItem, error) {
	var out []runJobItem
	pageToken := ""
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, fmt.Errorf("gcloud: cloud run jobs in %q: still paging after %d pages, refusing to follow the cursor further", location, maxListPages)
		}
		path := "/v2/projects/" + url.PathEscape(c.projectID) + "/locations/" + url.PathEscape(location) + "/jobs?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var resp struct {
			Jobs          []runJobItem `json:"jobs"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Jobs...)
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

func buildServiceResources(accountID uuid.UUID, items []runServiceItem) []domain.CloudResource {
	out := make([]domain.CloudResource, 0, len(items))
	for _, item := range items {
		projectID, location, short, ok := parseRunResourceName(item.Name, "services")
		if !ok {
			continue
		}
		out = append(out, domain.CloudResource{
			AccountID: accountID,
			Provider:  domain.CloudGCP,
			Ref: domain.CloudResourceRef{
				Kind:   domain.CloudResourceCloudRunService,
				ID:     item.Name,
				Name:   short,
				Region: location,
				Extra:  map[string]string{"project_id": projectID},
			},
			URL:    item.URI,
			Labels: mergeLabels(item.Labels, firstImage(item.Template.Containers)),
		})
	}
	return out
}

func buildJobResources(accountID uuid.UUID, items []runJobItem) []domain.CloudResource {
	out := make([]domain.CloudResource, 0, len(items))
	for _, item := range items {
		projectID, location, short, ok := parseRunResourceName(item.Name, "jobs")
		if !ok {
			continue
		}
		out = append(out, domain.CloudResource{
			AccountID: accountID,
			Provider:  domain.CloudGCP,
			Ref: domain.CloudResourceRef{
				Kind:   domain.CloudResourceCloudRunJob,
				ID:     item.Name,
				Name:   short,
				Region: location,
				Extra:  map[string]string{"project_id": projectID},
			},
			Labels: mergeLabels(item.Labels, firstImage(item.Template.Template.Containers)),
		})
	}
	return out
}

func (p *Provider) Resource(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return domain.CloudResourceDetail{}, err
	}
	switch ref.Kind {
	case domain.CloudResourceCloudRunService:
		return serviceDetail(ctx, c, cred.AccountID, ref)
	case domain.CloudResourceCloudRunJob:
		return jobDetail(ctx, c, cred.AccountID, ref)
	default:
		return domain.CloudResourceDetail{}, fmt.Errorf("gcloud: unsupported resource kind %q: %w", ref.Kind, port.ErrUnsupported)
	}
}

func serviceDetail(ctx context.Context, c *Client, accountID uuid.UUID, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	var item runServiceItem
	if err := c.get(ctx, c.runBaseURL, "/v2/"+ref.ID, &item); err != nil {
		return domain.CloudResourceDetail{}, classifyRunError(err)
	}
	if item.Name == "" {
		item.Name = ref.ID
	}

	projectID := projectIDFromRef(ref, c)
	ready := readyCondition(item.Conditions, item.TerminalCondition)

	detail := domain.CloudResourceDetail{
		CloudResource: domain.CloudResource{
			AccountID: accountID,
			Provider:  domain.CloudGCP,
			Ref:       ref,
			URL:       item.URI,
			Labels:    mergeLabels(item.Labels, firstImage(item.Template.Containers)),
		},
		Status:       conditionStatus(ready.State),
		StatusDetail: ready.Message,
		Revision:     shortName(item.LatestReadyRevision),
		ConsoleURL:   fmt.Sprintf("https://console.cloud.google.com/run/detail/%s/%s/metrics?project=%s", ref.Region, ref.Name, projectID),
		Facts:        serviceFacts(ref, item),
	}

	if revisions, err := serviceRevisions(ctx, c, ref, 1); err == nil && len(revisions) > 0 {
		d := mapRevisionToDeployment(revisions[0], ref, projectID)
		detail.LatestDeployment = &d
	}
	return detail, nil
}

func serviceFacts(ref domain.CloudResourceRef, item runServiceItem) []domain.KeyValue {
	facts := []domain.KeyValue{{Label: "Region", Value: ref.Region}}
	if image := firstImage(item.Template.Containers); image != "" {
		facts = append(facts, domain.KeyValue{Label: "Image", Value: image})
	}
	for _, t := range item.TrafficStatuses {
		rev := shortName(t.Revision)
		if rev == "" {
			rev = shortName(item.LatestReadyRevision)
		}
		facts = append(facts, domain.KeyValue{Label: "Traffic", Value: fmt.Sprintf("%s: %d%%", rev, t.Percent)})
	}
	facts = append(facts,
		domain.KeyValue{Label: "Min instances", Value: strconv.Itoa(item.Template.Scaling.MinInstanceCount)},
		domain.KeyValue{Label: "Max instances", Value: strconv.Itoa(item.Template.Scaling.MaxInstanceCount)},
	)
	if item.Ingress != "" {
		facts = append(facts, domain.KeyValue{Label: "Ingress", Value: item.Ingress})
	}
	return facts
}

func jobDetail(ctx context.Context, c *Client, accountID uuid.UUID, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	projectID := projectIDFromRef(ref, c)

	executions, err := jobExecutionsPage(ctx, c, ref, 1)
	if err != nil {
		return domain.CloudResourceDetail{}, classifyRunError(err)
	}

	detail := domain.CloudResourceDetail{
		CloudResource: domain.CloudResource{
			AccountID: accountID,
			Provider:  domain.CloudGCP,
			Ref:       ref,
		},
		Status:     domain.CloudStatusUnknown,
		ConsoleURL: fmt.Sprintf("https://console.cloud.google.com/run/jobs/details/%s/%s/executions?project=%s", ref.Region, ref.Name, projectID),
		Facts:      []domain.KeyValue{{Label: "Region", Value: ref.Region}},
	}

	if len(executions) == 0 {
		detail.StatusDetail = "no executions yet"
		return detail, nil
	}

	exec := executions[0]
	ready := executionReadyCondition(exec.Conditions)
	detail.Status = conditionStatus(ready.State)
	detail.StatusDetail = ready.Message
	if image := firstImage(exec.Template.Containers); image != "" {
		detail.Facts = append(detail.Facts, domain.KeyValue{Label: "Image", Value: image})
		detail.Labels = mergeLabels(exec.Labels, image)
	}
	d := mapExecutionToDeployment(exec, ref, projectID)
	detail.LatestDeployment = &d
	return detail, nil
}

func serviceRevisions(ctx context.Context, c *Client, ref domain.CloudResourceRef, limit int) ([]runRevisionItem, error) {
	if limit <= 0 {
		limit = 20
	}
	path := "/v2/" + ref.ID + "/revisions?pageSize=" + strconv.Itoa(limit)
	var resp struct {
		Revisions []runRevisionItem `json:"revisions"`
	}
	if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
		return nil, err
	}
	return resp.Revisions, nil
}

func jobExecutionsPage(ctx context.Context, c *Client, ref domain.CloudResourceRef, limit int) ([]runExecutionItem, error) {
	if limit <= 0 {
		limit = 20
	}
	path := "/v2/" + ref.ID + "/executions?pageSize=" + strconv.Itoa(limit)
	var resp struct {
		Executions []runExecutionItem `json:"executions"`
	}
	if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
		return nil, err
	}
	return resp.Executions, nil
}

func (p *Provider) Deployments(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, _ domain.DeployEnvironment, limit int) ([]domain.CloudDeployment, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return nil, err
	}
	projectID := projectIDFromRef(ref, c)

	switch ref.Kind {
	case domain.CloudResourceCloudRunService:
		items, err := serviceRevisions(ctx, c, ref, limit)
		if err != nil {
			return nil, classifyRunError(err)
		}
		out := make([]domain.CloudDeployment, 0, len(items))
		for _, it := range items {
			out = append(out, mapRevisionToDeployment(it, ref, projectID))
		}
		return out, nil
	case domain.CloudResourceCloudRunJob:
		items, err := jobExecutionsPage(ctx, c, ref, limit)
		if err != nil {
			return nil, classifyRunError(err)
		}
		out := make([]domain.CloudDeployment, 0, len(items))
		for _, it := range items {
			out = append(out, mapExecutionToDeployment(it, ref, projectID))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("gcloud: unsupported resource kind %q: %w", ref.Kind, port.ErrUnsupported)
	}
}

func mapRevisionToDeployment(item runRevisionItem, ref domain.CloudResourceRef, projectID string) domain.CloudDeployment {
	ready := readyCondition(item.Conditions, runCondition{})
	d := domain.CloudDeployment{
		ID:          shortName(item.Name),
		Status:      deploymentStatus(ready.State),
		Environment: domain.EnvironmentProduction,
		CommitSHA:   revisionCommitSHA(item.Labels, item.Annotations),
		InspectURL:  fmt.Sprintf("https://console.cloud.google.com/run/detail/%s/%s/revisions?project=%s", ref.Region, ref.Name, projectID),
	}
	if t, err := time.Parse(time.RFC3339, item.CreateTime); err == nil {
		d.CreatedAt = t
	}
	if ready.LastTransitionTime != "" {
		if t, err := time.Parse(time.RFC3339, ready.LastTransitionTime); err == nil {
			d.ReadyAt = &t
		}
	}
	return d
}

func mapExecutionToDeployment(exec runExecutionItem, ref domain.CloudResourceRef, projectID string) domain.CloudDeployment {
	ready := executionReadyCondition(exec.Conditions)
	status := deploymentStatus(ready.State)
	if status == domain.CloudDeployError && exec.CancelledCount > 0 && exec.FailedCount == 0 {
		status = domain.CloudDeployCanceled
	}
	d := domain.CloudDeployment{
		ID:          shortName(exec.Name),
		Status:      status,
		Environment: domain.EnvironmentProduction,
		CommitSHA:   revisionCommitSHA(exec.Labels, exec.Annotations),
		InspectURL:  fmt.Sprintf("https://console.cloud.google.com/run/jobs/details/%s/%s/executions?project=%s", ref.Region, ref.Name, projectID),
	}
	if t, err := time.Parse(time.RFC3339, exec.CreateTime); err == nil {
		d.CreatedAt = t
	}
	completion := exec.CompletionTime
	if completion == "" {
		completion = ready.LastTransitionTime
	}
	if completion != "" {
		if t, err := time.Parse(time.RFC3339, completion); err == nil {
			d.ReadyAt = &t
		}
	}
	return d
}
