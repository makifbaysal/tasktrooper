package datasource_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// memSources is the data source table and its cloud links as the database
// keeps them, including ON DELETE SET NULL on a link.
type memSources struct {
	mu       sync.Mutex
	sources  []domain.DataSource
	links    []domain.CloudSourceLink
	accounts []domain.CloudAccount
}

func (m *memSources) ListDataSources(context.Context) ([]domain.DataSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.DataSource(nil), m.sources...), nil
}

func (m *memSources) GetDataSource(_ context.Context, id uuid.UUID) (domain.DataSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ds := range m.sources {
		if ds.ID == id {
			return ds, nil
		}
	}
	return domain.DataSource{}, port.ErrNotFound
}

func (m *memSources) CreateDataSource(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ds.ID = uuid.New()
	m.sources = append(m.sources, ds)
	return ds, nil
}

func (m *memSources) UpdateDataSource(context.Context, domain.DataSource, map[string]string) (domain.DataSource, error) {
	return domain.DataSource{}, nil
}

func (m *memSources) RecordVerification(_ context.Context, id uuid.UUID, v domain.DataSourceVerification, _ time.Time) (domain.DataSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sources {
		if m.sources[i].ID == id {
			m.sources[i].Status, m.sources[i].StatusDetail, m.sources[i].Info = v.Status, v.StatusDetail, v.Info
			return m.sources[i], nil
		}
	}
	return domain.DataSource{}, port.ErrNotFound
}

func (m *memSources) DeleteDataSource(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, ds := range m.sources {
		if ds.ID == id {
			m.sources = append(m.sources[:i], m.sources[i+1:]...)
			for j := range m.links {
				if m.links[j].DataSourceID != nil && *m.links[j].DataSourceID == id {
					m.links[j].DataSourceID = nil
				}
			}
			return nil
		}
	}
	return port.ErrNotFound
}

func (m *memSources) DataSourceSecrets(context.Context, uuid.UUID) (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *memSources) ListCloudSourceLinks(context.Context) ([]domain.CloudSourceLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.CloudSourceLink(nil), m.links...), nil
}

func (m *memSources) LinkCloudSource(_ context.Context, accountID uuid.UUID, kind domain.DataSourceKind, id uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.links {
		if l.CloudAccountID == accountID && l.Kind == kind {
			return false, nil
		}
	}
	m.links = append(m.links, domain.CloudSourceLink{CloudAccountID: accountID, Kind: kind, DataSourceID: &id})
	return true, nil
}

func (m *memSources) ListCloudAccounts(context.Context) ([]domain.CloudAccount, error) {
	return m.accounts, nil
}

type autoFixture struct {
	t     *testing.T
	mem   *memSources
	svc   *datasource.Service
	cloud *mocks.CloudAccountReader
}

func newAutoFixture(t *testing.T) *autoFixture {
	t.Helper()
	f := &autoFixture{t: t, mem: &memSources{}, cloud: &mocks.CloudAccountReader{}}
	var kinds []port.DataSourceKindAdapter
	for _, k := range []struct {
		kind     domain.DataSourceKind
		provider domain.CloudProviderKind
		label    string
	}{
		{domain.DataSourceKindVercel, domain.CloudVercel, "Vercel"},
		{domain.DataSourceKindGCPMonitoring, domain.CloudGCP, "Google Cloud Monitoring"},
		{domain.DataSourceKindCloudWatch, domain.CloudAWS, "Amazon CloudWatch"},
	} {
		a := &mocks.DataSourceKindAdapter{}
		a.On("Kind").Return(k.kind).Maybe()
		a.On("Schema").Return(domain.DataSourceSchema{Label: k.label, Fields: []domain.DataSourceField{
			{Key: "cloud_account_id", Type: domain.FieldCloudAccount, Provider: k.provider, Required: true},
		}}).Maybe()
		a.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
		a.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{Detail: k.label}, nil).Maybe()
		kinds = append(kinds, a)
	}
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store: f.mem, CloudAccounts: f.cloud, CloudAccountList: f.mem, CloudLinks: f.mem,
		Kinds: kinds, URLPolicy: &policy,
	})
	require.NoError(t, err)
	f.svc = svc
	return f
}

func (f *autoFixture) connect(provider domain.CloudProviderKind, label string) domain.CloudAccount {
	acct := domain.CloudAccount{ID: uuid.New(), Provider: provider, Label: label}
	f.cloud.On("GetCloudAccount", mock.Anything, acct.ID).Return(acct, nil).Maybe()
	f.cloud.On("CloudCredential", mock.Anything, acct.ID).Return(domain.CloudCredential{}, nil).Maybe()
	f.mem.accounts = append(f.mem.accounts, acct)
	return acct
}

func kindsOf(list []domain.DataSource) []domain.DataSourceKind {
	var out []domain.DataSourceKind
	for _, ds := range list {
		out = append(out, ds.Kind)
	}
	return out
}

func TestAConnectedAccountGetsItsSourceOnce(t *testing.T) {
	f := newAutoFixture(t)
	ctx := context.Background()
	acct := f.connect(domain.CloudVercel, "acme")

	f.svc.CloudAccountConnected(ctx, acct)
	f.svc.CloudAccountConnected(ctx, acct)
	f.svc.SyncCloudAccounts(ctx)

	got, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, domain.DataSourceKindVercel, got[0].Kind)
	assert.Equal(t, map[string]string{"cloud_account_id": acct.ID.String()}, got[0].Config)
	assert.Equal(t, domain.DataSourceOK, got[0].Status, "the source is verified when made")
	assert.Equal(t, "Vercel · acme", got[0].Name)
}

func TestEveryProviderGetsItsOwnKind(t *testing.T) {
	f := newAutoFixture(t)
	f.connect(domain.CloudVercel, "v")
	f.connect(domain.CloudGCP, "g")
	f.connect(domain.CloudAWS, "a")

	f.svc.SyncCloudAccounts(context.Background())

	got, _ := f.svc.List(context.Background())
	assert.ElementsMatch(t, []domain.DataSourceKind{
		domain.DataSourceKindVercel, domain.DataSourceKindGCPMonitoring, domain.DataSourceKindCloudWatch,
	}, kindsOf(got))
}

func TestASourceAPersonAddedForTheAccountIsNotDuplicated(t *testing.T) {
	f := newAutoFixture(t)
	ctx := context.Background()
	acct := f.connect(domain.CloudGCP, "g")
	_, err := f.svc.Create(ctx, domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": acct.ID.String()},
	})
	require.NoError(t, err)

	f.svc.SyncCloudAccounts(ctx)

	got, _ := f.svc.List(ctx)
	assert.Len(t, got, 1)
}

func TestADeletedSourceDoesNotComeBack(t *testing.T) {
	f := newAutoFixture(t)
	ctx := context.Background()
	acct := f.connect(domain.CloudAWS, "a")
	f.svc.SyncCloudAccounts(ctx)
	got, _ := f.svc.List(ctx)
	require.Len(t, got, 1)

	require.NoError(t, f.svc.Delete(ctx, got[0].ID))
	f.svc.SyncCloudAccounts(ctx)
	f.svc.CloudAccountConnected(ctx, acct)

	got, _ = f.svc.List(ctx)
	assert.Empty(t, got)
}

func TestDisconnectingAnAccountDeletesOnlyItsOwnMadeSources(t *testing.T) {
	f := newAutoFixture(t)
	ctx := context.Background()
	gone := f.connect(domain.CloudVercel, "gone")
	kept := f.connect(domain.CloudGCP, "kept")
	f.svc.SyncCloudAccounts(ctx)
	byHand, err := f.svc.Create(ctx, domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindVercel, Config: map[string]string{"cloud_account_id": gone.ID.String()},
	})
	require.NoError(t, err)

	f.svc.CloudAccountRemoved(ctx, gone.ID)

	got, _ := f.svc.List(ctx)
	var left []uuid.UUID
	for _, ds := range got {
		left = append(left, ds.ID)
	}
	assert.Len(t, got, 2)
	assert.Contains(t, left, byHand.ID, "a source added by hand stays")
	assert.Contains(t, kindsOf(got), domain.DataSourceKindGCPMonitoring)
	_ = kept
}

func TestWithoutLinkStoresNothingIsMade(t *testing.T) {
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{Store: &memSources{}, URLPolicy: &policy})
	require.NoError(t, err)
	svc.SyncCloudAccounts(context.Background())
	svc.CloudAccountConnected(context.Background(), domain.CloudAccount{ID: uuid.New(), Provider: domain.CloudVercel})
	svc.CloudAccountRemoved(context.Background(), uuid.New())
}
