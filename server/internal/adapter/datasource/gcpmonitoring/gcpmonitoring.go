// Package gcpmonitoring is the data source kind for Google Cloud Monitoring,
// read through its Prometheus-compatible query API with the service account
// of a connected Google Cloud account: Cloud Run, GKE, Pub/Sub, load
// balancing and every other Google metric, with nothing to deploy. In PromQL
// a metric type such as run.googleapis.com/request_count is
// run_googleapis_com:request_count, its metric and resource labels are labels
// (a metric label that collides with a resource label is metric_<name>), the
// resource type is the monitored_resource label, and a distribution is a
// histogram (_bucket, _count, _sum).
package gcpmonitoring

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/prometheus"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyAccount = "cloud_account_id"
	keyProject = "project_id"

	credentialField = "service_account_json"
	// MonitoringReadScope is the narrowest scope the query API takes.
	MonitoringReadScope = "https://www.googleapis.com/auth/monitoring.read"
	defaultBaseURL      = "https://monitoring.googleapis.com"

	tokenTimeout = 15 * time.Second
	// tokenRefreshMargin renews a token this long before it expires, so one
	// handed to a query does not lapse in flight.
	tokenRefreshMargin = time.Minute
	maxCachedTokens    = 64
	maxErrorText       = 300
)

var projectPattern = regexp.MustCompile(`^([a-z0-9.-]+:)?[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// Minter exchanges a service account key for an access token of scope;
// gcloud.MintAccessToken unless a test replaces it.
type Minter func(ctx context.Context, client *http.Client, serviceAccountJSON, scope string) (token string, expiresAt time.Time, err error)

type cachedToken struct {
	token   string
	expires time.Time
}

type Adapter struct {
	api      *prometheus.API
	policy   urlguard.Policy
	accounts port.CloudAccountReader
	baseURL  string
	mint     Minter
	now      func() time.Time

	mu sync.Mutex
	// tokens is keyed by a hash of the key file, so a rotated key mints anew
	// and no key material is held here beyond what the token is.
	tokens map[[sha256.Size]byte]cachedToken
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination until SetURLPolicy
// and reads no account until SetCloudAccounts; datasource.NewService sets
// both.
func New() *Adapter {
	return &Adapter{
		api:     prometheus.NewAPI(),
		baseURL: defaultBaseURL,
		mint:    gcloud.MintAccessToken,
		now:     time.Now,
		tokens:  map[[sha256.Size]byte]cachedToken{},
	}
}

// SetURLPolicy applies to the token exchange and the query API alike: the
// API's address is fixed, but it is dialled like any other.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) {
	a.policy = p
	a.api.SetURLPolicy(p)
}

func (a *Adapter) SetCloudAccounts(r port.CloudAccountReader) { a.accounts = r }

func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetMinter(m Minter) { a.mint = m }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) SetMaxInventory(n int) { a.api.SetMaxInventory(n) }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindGCPMonitoring }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Google Cloud Monitoring",
		Description: "Cloud Run, GKE, Pub/Sub, load balancers and every other Google Cloud metric, read with a connected Google Cloud account. Nothing to install.",
		Fields: []domain.DataSourceField{
			{
				Key: keyAccount, Label: "Google Cloud account", Type: domain.FieldCloudAccount,
				Provider: domain.CloudGCP, Required: true,
				Help: "Its service account needs the Monitoring Viewer role (roles/monitoring.viewer) on the project.",
			},
			{
				Key: keyProject, Label: "Project", Type: domain.FieldText,
				Placeholder: "the account's project",
				Help:        "The project whose metrics to read. Empty reads the account's own; a scoping project also reads the projects its metrics scope holds.",
			},
		},
	}
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	ep, project, err := a.endpoint(ctx, ds)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	if err := a.api.Probe(ctx, ep); err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: "Google Cloud Monitoring · " + project}, nil
}

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, _ map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	ep, _, err := a.endpoint(ctx, ds)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	return a.api.QueryRange(ctx, ep, q)
}

func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, _ map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	ep, _, err := a.endpoint(ctx, ds)
	if err != nil {
		return domain.TableResult{}, err
	}
	return a.api.Instant(ctx, ep, q)
}

func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.Inventory, error) {
	ep, _, err := a.endpoint(ctx, ds)
	if err != nil {
		return domain.Inventory{}, err
	}
	return a.api.MetricNames(ctx, ep)
}

func (a *Adapter) endpoint(ctx context.Context, ds domain.DataSource) (prometheus.Endpoint, string, error) {
	cred, err := a.credential(ctx, ds)
	if err != nil {
		return prometheus.Endpoint{}, "", err
	}
	key := cred.Fields[credentialField]
	project := strings.TrimSpace(ds.Config[keyProject])
	if project == "" {
		project = cred.Meta["project_id"]
	}
	if project == "" {
		project = gcloud.ServiceAccountProjectID(key)
	}
	switch {
	case project == "":
		return prometheus.Endpoint{}, "", errors.New("no project: the account names none, so set the project")
	case !projectPattern.MatchString(project):
		return prometheus.Endpoint{}, "", fmt.Errorf("%q is not a Google Cloud project id", project)
	}
	tok, err := a.token(ctx, key)
	if err != nil {
		return prometheus.Endpoint{}, "", err
	}
	return prometheus.Endpoint{
		Base:    a.baseURL + "/v1/projects/" + url.PathEscape(project) + "/location/global/prometheus",
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + tok}},
		Masks:   []string{tok},
	}, project, nil
}

func (a *Adapter) credential(ctx context.Context, ds domain.DataSource) (domain.CloudCredential, error) {
	if a.accounts == nil {
		return domain.CloudCredential{}, errors.New("cloud accounts are not available here")
	}
	id, err := uuid.Parse(ds.Config[keyAccount])
	if err != nil {
		return domain.CloudCredential{}, errors.New("no Google Cloud account is selected")
	}
	cred, err := a.accounts.CloudCredential(ctx, id)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return domain.CloudCredential{}, errors.New("the Google Cloud account is no longer connected")
	case err != nil:
		return domain.CloudCredential{}, fmt.Errorf("reading the Google Cloud account: %w", err)
	case cred.Provider != domain.CloudGCP:
		return domain.CloudCredential{}, errors.New("the selected cloud account is not a Google Cloud account")
	case cred.Fields[credentialField] == "":
		return domain.CloudCredential{}, errors.New("the Google Cloud account has no service account key")
	}
	return cred, nil
}

// token is cached until shortly before it expires. Two queries missing the
// cache together may both mint; that costs a token, not correctness.
func (a *Adapter) token(ctx context.Context, serviceAccountJSON string) (string, error) {
	key := sha256.Sum256([]byte(serviceAccountJSON))
	a.mu.Lock()
	cached, ok := a.tokens[key]
	a.mu.Unlock()
	if ok && a.now().Before(cached.expires.Add(-tokenRefreshMargin)) {
		return cached.token, nil
	}

	client := a.policy.Client(tokenTimeout)
	defer client.CloseIdleConnections()
	tok, exp, err := a.mint(ctx, client, serviceAccountJSON, MonitoringReadScope)
	if err != nil {
		return "", mintError(err)
	}

	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, t := range a.tokens {
		if !now.Before(t.expires) {
			delete(a.tokens, k)
		}
	}
	if len(a.tokens) >= maxCachedTokens {
		clear(a.tokens)
	}
	a.tokens[key] = cachedToken{token: tok, expires: exp}
	return tok, nil
}

// mintError keeps the status and OAuth code the minter reports (never the
// token endpoint's own text, which it only logs) and drops the token URL
// net/http adds.
func mintError(err error) error {
	switch {
	case errors.Is(err, gcloud.ErrTokenURINotGoogle):
		return errors.New("google sign-in: the key's token_uri is not Google's token endpoint")
	case errors.Is(err, urlguard.ErrBlocked):
		return errors.New("google sign-in: the key's token_uri is not an allowed destination")
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if r := []rune(msg); len(r) > maxErrorText {
		msg = string(r[:maxErrorText]) + "…"
	}
	return fmt.Errorf("google sign-in for the cloud account failed: %s", msg)
}
