package datasource_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// cloudKind records the reader the service hands it; that hand-over is what
// SetCloudAccounts is for, so it is the behavior under test, not a stub.
type cloudKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	accounts port.CloudAccountReader
}

func (k *cloudKind) SetCloudAccounts(r port.CloudAccountReader) { k.accounts = r }

func cloudSchema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label: "Google Cloud Monitoring",
		Fields: []domain.DataSourceField{
			{Key: "cloud_account_id", Type: domain.FieldCloudAccount, Provider: domain.CloudGCP, Required: true},
			{Key: "project_id", Type: domain.FieldText},
		},
	}
}

type CloudAccountFieldSuite struct {
	suite.Suite
	ctx      context.Context
	store    *mocks.DataSourceStore
	accounts *mocks.CloudAccountReader
	kind     *cloudKind
	svc      *datasource.Service
}

func TestCloudAccountFieldSuite(t *testing.T) { suite.Run(t, new(CloudAccountFieldSuite)) }

func (s *CloudAccountFieldSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = &mocks.DataSourceStore{}
	s.accounts = &mocks.CloudAccountReader{}
	s.kind = &cloudKind{DataSourceKindAdapter: &mocks.DataSourceKindAdapter{}, MetricsQuerier: &mocks.MetricsQuerier{}}
	s.kind.DataSourceKindAdapter.On("Kind").Return(domain.DataSourceKindGCPMonitoring).Maybe()
	s.kind.DataSourceKindAdapter.On("Schema").Return(cloudSchema()).Maybe()
	s.kind.DataSourceKindAdapter.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:         s.store,
		CloudAccounts: s.accounts,
		Kinds:         []port.DataSourceKindAdapter{s.kind},
		URLPolicy:     &policy,
	})
	s.Require().NoError(err)
	s.svc = svc
}

func (s *CloudAccountFieldSuite) TearDownTest() {
	s.store.AssertExpectations(s.T())
	s.accounts.AssertExpectations(s.T())
	s.kind.DataSourceKindAdapter.AssertExpectations(s.T())
	s.kind.MetricsQuerier.AssertExpectations(s.T())
}

func (s *CloudAccountFieldSuite) TestTheServiceHandsKindsItsAccountReader() {
	s.Same(s.accounts, s.kind.accounts)
}

func (s *CloudAccountFieldSuite) TestCreateStoresOnlyTheAccountIDAndNamesTheSourceAfterIt() {
	accountID := uuid.New()
	id := uuid.New()
	s.accounts.On("GetCloudAccount", mock.Anything, accountID).
		Return(domain.CloudAccount{ID: accountID, Provider: domain.CloudGCP, Label: "acme-prod"}, nil)
	s.accounts.On("CloudCredential", mock.Anything, accountID).Return(domain.CloudCredential{}, nil).Maybe()
	s.store.On("CreateDataSource", mock.Anything, domain.DataSource{
		Kind:   domain.DataSourceKindGCPMonitoring,
		Name:   "Google Cloud Monitoring · acme-prod",
		Config: map[string]string{"cloud_account_id": accountID.String()},
		Status: domain.DataSourceUnverified,
	}, map[string]string{}).Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
		ds.ID = id
		return ds, nil
	})
	s.kind.DataSourceKindAdapter.On("Verify", mock.Anything, mock.Anything, mock.Anything).
		Return(domain.DataSourceInfo{Detail: "Google Cloud Monitoring · acme-prod"}, nil)
	s.store.On("RecordVerification", mock.Anything, id, mock.Anything, mock.Anything).Return(recorded(nil))

	got, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
		Kind:   domain.DataSourceKindGCPMonitoring,
		Config: map[string]string{"cloud_account_id": " " + strings.ToUpper(accountID.String()) + " "},
	})

	s.Require().NoError(err)
	s.Equal(domain.DataSourceOK, got.Status)
}

func (s *CloudAccountFieldSuite) TestCreateRefusesAnAccountThatDoesNotFit() {
	missing, aws, broken := uuid.New(), uuid.New(), uuid.New()
	s.accounts.On("GetCloudAccount", mock.Anything, missing).Return(domain.CloudAccount{}, port.ErrNotFound)
	s.accounts.On("GetCloudAccount", mock.Anything, aws).Return(domain.CloudAccount{ID: aws, Provider: domain.CloudAWS}, nil)
	storeDown := errors.New("connection reset")
	s.accounts.On("GetCloudAccount", mock.Anything, broken).Return(domain.CloudAccount{}, storeDown)

	cases := []struct {
		name    string
		account string
		want    datasource.FieldErrors
	}{
		{"none", "", datasource.FieldErrors{"cloud_account_id": datasource.FieldRequired}},
		{"not an id", "acme", datasource.FieldErrors{"cloud_account_id": datasource.FieldInvalid}},
		{"no such account", missing.String(), datasource.FieldErrors{"cloud_account_id": datasource.FieldNotFound}},
		{"another provider's", aws.String(), datasource.FieldErrors{"cloud_account_id": datasource.FieldWrongProvider}},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
				Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": tc.account},
			})
			var fe datasource.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tc.want, fe)
		})
	}

	_, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": broken.String()},
	})
	s.ErrorIs(err, storeDown)
	var fe datasource.FieldErrors
	s.False(errors.As(err, &fe), "a store failure is not the caller's mistake")
}

func (s *CloudAccountFieldSuite) TestAnErrorQuotingTheAccountsCredentialIsMasked() {
	accountID, id := uuid.New(), uuid.New()
	ds := domain.DataSource{ID: id, Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": accountID.String()}}
	s.store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{}, nil)
	s.accounts.On("CloudCredential", mock.Anything, accountID).Return(domain.CloudCredential{
		AccountID: accountID, Provider: domain.CloudAWS,
		Meta:   map[string]string{"region": "eu-central-1"},
		Fields: map[string]string{"secret_access_key": "wJalrXUtnFEMI-K7MDENG", "region": "eu-central-1"},
	}, nil)
	s.kind.MetricsQuerier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, errors.New("monitoring.eu-central-1.amazonaws.com said key wJalrXUtnFEMI-K7MDENG is bad"))

	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	_, err := s.svc.Query(s.ctx, id, domain.SeriesQuery{Query: "up", From: from, To: from.Add(time.Hour), Step: time.Minute})

	s.Require().Error(err)
	s.ErrorIs(err, datasource.ErrUpstream)
	s.NotContains(err.Error(), "wJalrXUtnFEMI-K7MDENG")
	s.Contains(err.Error(), "eu-central-1", "a value the account reports as non-secret stays readable")
}

func TestWithoutAnAccountReaderACloudAccountFieldTakesNoValue(t *testing.T) {
	kind := &mocks.DataSourceKindAdapter{}
	kind.On("Kind").Return(domain.DataSourceKindGCPMonitoring)
	kind.On("Schema").Return(cloudSchema())
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{Store: &mocks.DataSourceStore{}, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy})
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": uuid.NewString()},
	})

	var fe datasource.FieldErrors
	require.ErrorAs(t, err, &fe)
	require.Equal(t, datasource.FieldErrors{"cloud_account_id": datasource.FieldNotPermitted}, fe)
}
