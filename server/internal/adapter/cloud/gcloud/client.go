package gcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultRunBaseURL       = "https://run.googleapis.com"
	defaultContainerBaseURL = "https://container.googleapis.com"
)

const tokenRefreshMargin = 30 * time.Second

const maxListPages = 50

const maxLocationFanOut = 8

type cachedToken struct {
	token string
	exp   time.Time
}

type Client struct {
	sa        parsedServiceAccount
	projectID string

	runBaseURL       string
	containerBaseURL string
	tokenURL         string
	httpClient       *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken
}

func New(cred domain.GCloudCredential) (*Client, error) {
	raw := cred.Data["service_account_json"]
	if raw == "" {
		return nil, errors.New("gcloud: credential missing service_account_json")
	}
	sa, err := parseServiceAccountJSON(raw)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(cred.ProjectID)
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("gcloud: no project id — the key file names none and none was supplied")
	}
	return &Client{
		sa:               sa,
		projectID:        projectID,
		runBaseURL:       defaultRunBaseURL,
		containerBaseURL: defaultContainerBaseURL,
		tokenURL:         sa.TokenURL,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		tokens:           make(map[string]cachedToken, 1),
	}, nil
}

func (c *Client) Identity() domain.GCloudIdentity {
	return domain.GCloudIdentity{ProjectID: c.projectID, ClientEmail: c.sa.ClientEmail}
}

func (c *Client) SetRunBaseURL(u string)       { c.runBaseURL = u }
func (c *Client) SetContainerBaseURL(u string) { c.containerBaseURL = u }
func (c *Client) SetTokenURL(u string)         { c.tokenURL = u }

func (c *Client) bearerToken(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.tokens[scope]; ok && time.Now().Before(cached.exp.Add(-tokenRefreshMargin)) {
		return cached.token, nil
	}
	tok, exp, err := fetchAccessToken(ctx, c.httpClient, c.tokenURL, c.sa.ClientEmail, scope, c.sa.PrivateKey)
	if err != nil {
		return "", err
	}
	c.tokens[scope] = cachedToken{token: tok, exp: exp}
	return tok, nil
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gcloud api: %d %s", e.Status, e.Body)
}

func (c *Client) get(ctx context.Context, baseURL, path string, out any) error {
	tok, err := c.bearerToken(ctx, cloudPlatformScope)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Body: domain.TruncateHead(string(data), 500)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func classifyListing(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrGCloudListingUnavailable)
	}
	return err
}

func classifyGet(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrNotFound)
	}
	return classifyListing(err)
}

func (c *Client) ValidateAuth(ctx context.Context) error {
	_, err := c.bearerToken(ctx, cloudPlatformScope)
	return err
}

type runService struct {
	Name                  string `json:"name"`
	URI                   string `json:"uri"`
	LatestReadyRevision   string `json:"latestReadyRevision"`
	LatestCreatedRevision string `json:"latestCreatedRevision"`
	UpdateTime            string `json:"updateTime"`
	Template              struct {
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"template"`
	TrafficStatuses []struct {
		Type     string `json:"type"`
		Revision string `json:"revision"`
		Percent  int    `json:"percent"`
		Tag      string `json:"tag"`
		URI      string `json:"uri"`
	} `json:"trafficStatuses"`
	TerminalCondition struct {
		Type    string `json:"type"`
		State   string `json:"state"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"terminalCondition"`
}

func (c *Client) ListCloudRunServices(ctx context.Context) (domain.GCloudResourceList, error) {
	refs, err := c.listCloudRunIn(ctx, "-")
	if err == nil {
		sortRefs(refs)
		return domain.GCloudResourceList{Resources: refs}, nil
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || !wildcardRejected(apiErr.Status) {
		return domain.GCloudResourceList{}, classifyListing(err)
	}

	locations, err := c.cloudRunLocations(ctx)
	if err != nil {
		return domain.GCloudResourceList{}, classifyListing(err)
	}
	if len(locations) == 0 {
		return domain.GCloudResourceList{Resources: []domain.GCloudResourceRef{}}, nil
	}

	var (
		mu          sync.Mutex
		all         []domain.GCloudResourceRef
		unreachable []string
		forbidden   int
		wg          sync.WaitGroup
	)
	sem := make(chan struct{}, maxLocationFanOut)
	for _, loc := range locations {
		wg.Add(1)
		go func(loc string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			found, err := c.listCloudRunIn(ctx, loc)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				unreachable = append(unreachable, loc)
				var apiErr *apiError
				if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
					forbidden++
				}
				return
			}
			all = append(all, found...)
		}(loc)
	}
	wg.Wait()

	if forbidden == len(locations) {
		return domain.GCloudResourceList{}, fmt.Errorf("gcloud: cloud run listing refused in every location: %w", port.ErrGCloudListingUnavailable)
	}

	sortRefs(all)
	sort.Strings(unreachable)
	return domain.GCloudResourceList{Resources: all, UnreachableLocations: unreachable}, nil
}

func wildcardRejected(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound
}

func (c *Client) listCloudRunIn(ctx context.Context, location string) ([]domain.GCloudResourceRef, error) {
	var out []domain.GCloudResourceRef
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
			Services      []runService `json:"services"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		for _, svc := range resp.Services {
			out = append(out, runServiceRef(svc))
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

func (c *Client) cloudRunLocations(ctx context.Context) ([]string, error) {
	var out []string
	pageToken := ""
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, fmt.Errorf("gcloud: cloud run locations: still paging after %d pages, refusing to follow the cursor further", maxListPages)
		}
		path := "/v2/projects/" + url.PathEscape(c.projectID) + "/locations?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var resp struct {
			Locations []struct {
				LocationID string `json:"locationId"`
			} `json:"locations"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		for _, loc := range resp.Locations {
			if loc.LocationID != "" {
				out = append(out, loc.LocationID)
			}
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

func runServiceRef(svc runService) domain.GCloudResourceRef {
	parsed, err := domain.ParseGCloudResourceName(svc.Name)
	ref := domain.GCloudResourceRef{
		Type:  domain.GCloudResourceCloudRun,
		Name:  svc.Name,
		URI:   svc.URI,
		State: svc.TerminalCondition.State,
	}
	if err == nil {
		ref.ProjectID = parsed.ProjectID
		ref.Location = parsed.Location
		ref.DisplayName = parsed.ShortName
	} else {
		ref.DisplayName = shortName(svc.Name)
	}
	return ref
}

func (c *Client) CloudRunService(ctx context.Context, name string) (domain.CloudRunServiceDetail, error) {
	parsed, err := domain.ParseGCloudResourceName(name)
	if err != nil {
		return domain.CloudRunServiceDetail{}, err
	}
	if parsed.Type != domain.GCloudResourceCloudRun {
		return domain.CloudRunServiceDetail{}, fmt.Errorf("%w: %q is not a cloud run service", domain.ErrInvalidGCloudResource, name)
	}

	var svc runService
	if err := c.get(ctx, c.runBaseURL, "/v2/"+name, &svc); err != nil {
		return domain.CloudRunServiceDetail{}, classifyGet(err)
	}
	if svc.Name == "" {
		svc.Name = name
	}

	detail := domain.CloudRunServiceDetail{
		Ref:                   runServiceRef(svc),
		LatestReadyRevision:   shortName(svc.LatestReadyRevision),
		LatestCreatedRevision: shortName(svc.LatestCreatedRevision),
		Ready:                 svc.TerminalCondition.State,
		ReadyReason:           svc.TerminalCondition.Reason,
		ReadyMessage:          svc.TerminalCondition.Message,
	}
	if len(svc.Template.Containers) > 0 {
		detail.Image = svc.Template.Containers[0].Image
	}
	if t, err := time.Parse(time.RFC3339, svc.UpdateTime); err == nil {
		detail.UpdateTime = t
	}
	for _, t := range svc.TrafficStatuses {
		revision := shortName(t.Revision)
		if revision == "" && strings.HasSuffix(t.Type, "_LATEST") {
			revision = detail.LatestReadyRevision
		}
		detail.Traffic = append(detail.Traffic, domain.CloudRunTrafficTarget{
			Revision: revision,
			Percent:  t.Percent,
			Tag:      t.Tag,
			URI:      t.URI,
		})
	}
	return detail, nil
}

type gkeCluster struct {
	Name                 string `json:"name"`
	Location             string `json:"location"`
	Status               string `json:"status"`
	StatusMessage        string `json:"statusMessage"`
	CurrentMasterVersion string `json:"currentMasterVersion"`
	CurrentNodeCount     int    `json:"currentNodeCount"`
	Endpoint             string `json:"endpoint"`
	NodePools            []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		InitialNodeCount int    `json:"initialNodeCount"`
		Version          string `json:"version"`
		Config           struct {
			MachineType string `json:"machineType"`
		} `json:"config"`
	} `json:"nodePools"`
	Autopilot struct {
		Enabled bool `json:"enabled"`
	} `json:"autopilot"`
	PrivateClusterConfig struct {
		EnablePrivateEndpoint bool `json:"enablePrivateEndpoint"`
	} `json:"privateClusterConfig"`
}

func (c *Client) ListGKEClusters(ctx context.Context) (domain.GCloudResourceList, error) {
	var resp struct {
		Clusters     []gkeCluster `json:"clusters"`
		MissingZones []string     `json:"missingZones"`
	}
	path := "/v1/projects/" + url.PathEscape(c.projectID) + "/locations/-/clusters"
	if err := c.get(ctx, c.containerBaseURL, path, &resp); err != nil {
		return domain.GCloudResourceList{}, classifyListing(err)
	}

	out := make([]domain.GCloudResourceRef, 0, len(resp.Clusters))
	for _, cluster := range resp.Clusters {
		out = append(out, c.gkeClusterRef(cluster))
	}
	sortRefs(out)
	sort.Strings(resp.MissingZones)
	return domain.GCloudResourceList{Resources: out, UnreachableLocations: resp.MissingZones}, nil
}

func (c *Client) gkeClusterRef(cluster gkeCluster) domain.GCloudResourceRef {
	return domain.GCloudResourceRef{
		Type:        domain.GCloudResourceGKECluster,
		Name:        domain.GCloudResourceName(c.projectID, cluster.Location, "clusters", cluster.Name),
		DisplayName: cluster.Name,
		ProjectID:   c.projectID,
		Location:    cluster.Location,
		URI:         cluster.Endpoint,
		State:       cluster.Status,
	}
}

func (c *Client) GKECluster(ctx context.Context, name string) (domain.GKEClusterDetail, error) {
	parsed, err := domain.ParseGCloudResourceName(name)
	if err != nil {
		return domain.GKEClusterDetail{}, err
	}
	if parsed.Type != domain.GCloudResourceGKECluster {
		return domain.GKEClusterDetail{}, fmt.Errorf("%w: %q is not a gke cluster", domain.ErrInvalidGCloudResource, name)
	}

	var cluster gkeCluster
	if err := c.get(ctx, c.containerBaseURL, "/v1/"+name, &cluster); err != nil {
		return domain.GKEClusterDetail{}, classifyGet(err)
	}
	if cluster.Name == "" {
		cluster.Name = parsed.ShortName
	}
	if cluster.Location == "" {
		cluster.Location = parsed.Location
	}

	detail := domain.GKEClusterDetail{
		Ref:             c.gkeClusterRef(cluster),
		Status:          cluster.Status,
		StatusMessage:   cluster.StatusMessage,
		MasterVersion:   cluster.CurrentMasterVersion,
		NodeCount:       cluster.CurrentNodeCount,
		Autopilot:       cluster.Autopilot.Enabled,
		PrivateEndpoint: cluster.PrivateClusterConfig.EnablePrivateEndpoint,
	}
	for _, np := range cluster.NodePools {
		detail.NodePools = append(detail.NodePools, domain.GKENodePool{
			Name:        np.Name,
			Status:      np.Status,
			NodeCount:   np.InitialNodeCount,
			Version:     np.Version,
			MachineType: np.Config.MachineType,
		})
	}
	detail.WorkloadsAvailable = false
	detail.WorkloadsNote = gkeWorkloadsNote(detail)
	return detail, nil
}

func gkeWorkloadsNote(detail domain.GKEClusterDetail) string {
	if detail.PrivateEndpoint {
		return "the cluster's control plane has no public endpoint, so its Kubernetes API is not reachable from this server"
	}
	return "workload listing requires calling the cluster's own Kubernetes API, which this integration does not do"
}

func shortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func sortRefs(refs []domain.GCloudResourceRef) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
}
