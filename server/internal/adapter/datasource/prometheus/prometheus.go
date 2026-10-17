// Package prometheus is the data source kind for anything that speaks the
// Prometheus HTTP API: Prometheus itself, Mimir, Cortex, Thanos,
// VictoriaMetrics, Grafana Cloud, and Amazon Managed Prometheus behind a
// SigV4 proxy. Its API type is shared by every kind that serves that API
// under another name (Google Cloud Monitoring).
package prometheus

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL      = "url"
	keyAuth     = "auth"
	keyToken    = "token"
	keyUsername = "username"
	keyPassword = "password"
	keyHeaders  = "headers"

	authNone   = "none"
	authBearer = "bearer"
	authBasic  = "basic"
)

type Adapter struct {
	api *API
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy; datasource.NewService
// sets it from its required Deps.URLPolicy.
func New() *Adapter {
	return &Adapter{api: NewAPI()}
}

// SetURLPolicy replaces where a source URL may connect; every request is
// checked against it again at dial time.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.api.SetURLPolicy(p) }

func (a *Adapter) SetMaxInventory(n int) { a.api.SetMaxInventory(n) }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindPrometheus }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Prometheus compatible",
		Description: "Prometheus, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, or anything else serving the Prometheus HTTP API.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "URL", Type: domain.FieldURL, Required: true,
				Placeholder: "http://localhost:9090",
				Help:        "The API base URL, without /api/v1. Mimir serves it under /prometheus, Grafana Cloud under /api/prom.",
			},
			{
				Key: keyAuth, Label: "Authentication", Type: domain.FieldSelect,
				Options:     []string{authNone, authBearer, authBasic},
				Placeholder: authNone,
			},
			{
				Key: keyToken, Label: "Bearer token", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{keyAuth: authBearer},
			},
			{
				Key: keyUsername, Label: "Username", Type: domain.FieldText, Required: true,
				ShowWhen: map[string]string{keyAuth: authBasic},
				Help:     "For Grafana Cloud, the instance ID.",
			},
			{
				Key: keyPassword, Label: "Password", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{keyAuth: authBasic},
				Help:     "For Grafana Cloud, an access policy token with metrics:read.",
			},
			{
				Key: keyHeaders, Label: "Extra headers", Type: domain.FieldHeaders, Secret: true,
				Placeholder: "X-Scope-OrgID: tenant-1",
				Help:        "One \"Name: value\" per line, sent with every request — a Mimir or Cortex tenant, or an Authorization header of your own. Stored encrypted.",
			},
		},
	}
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	ep, err := endpoint(ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	return a.api.Verify(ctx, ep)
}

func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	ep, err := endpoint(ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	return a.api.QueryRange(ctx, ep, q)
}

func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error) {
	ep, err := endpoint(ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	return a.api.Instant(ctx, ep, q)
}

func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	ep, err := endpoint(ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	return a.api.MetricNames(ctx, ep)
}

// endpoint masks the encoded basic-auth value beside the password: a server
// quoting the header it refused quotes that, not the password.
func endpoint(ds domain.DataSource, secrets map[string]string) (Endpoint, error) {
	extra, err := domain.ParseHeaderLines(secrets[keyHeaders])
	if err != nil {
		return Endpoint{}, fmt.Errorf("extra headers: %w", err)
	}
	ep := Endpoint{Base: ds.Config[keyURL]}
	for _, v := range secrets {
		ep.Masks = append(ep.Masks, v)
	}
	switch ds.Config[keyAuth] {
	case authBearer:
		ep.Headers = append(ep.Headers, domain.HeaderLine{Name: "Authorization", Value: "Bearer " + secrets[keyToken]})
	case authBasic:
		basic := base64.StdEncoding.EncodeToString([]byte(ds.Config[keyUsername] + ":" + secrets[keyPassword]))
		ep.Headers = append(ep.Headers, domain.HeaderLine{Name: "Authorization", Value: "Basic " + basic})
		ep.Masks = append(ep.Masks, basic)
	}
	ep.Headers = append(ep.Headers, extra...)
	return ep, nil
}
