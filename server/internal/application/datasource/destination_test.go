package datasource_test

import (
	"context"
	"testing"

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

func brokerService(t *testing.T, policy urlguard.Policy) (*datasource.Service, *mocks.DataSourceStore, *mocks.DataSourceKindAdapter) {
	t.Helper()
	store := &mocks.DataSourceStore{}
	kind := &mocks.DataSourceKindAdapter{}
	kind.On("Kind").Return(domain.DataSourceKindKafka).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics, domain.CapabilityQueues}).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Kafka", Fields: []domain.DataSourceField{
		{Key: "bootstrap_servers", Type: domain.FieldText, Required: true, Destination: true},
		{Key: "username", Type: domain.FieldText},
		{Key: "password", Type: domain.FieldSecret},
	}}).Maybe()
	svc, err := datasource.NewService(datasource.Deps{Store: store, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy})
	require.NoError(t, err)
	t.Cleanup(func() { store.AssertExpectations(t); kind.AssertExpectations(t) })
	return svc, store, kind
}

func kafkaReq(servers string) domain.SaveDataSourceRequest {
	return domain.SaveDataSourceRequest{Kind: domain.DataSourceKindKafka, Config: map[string]string{"bootstrap_servers": servers}}
}

func TestADestinationFieldIsJudgedByTheURLPolicyWhenSaved(t *testing.T) {
	tests := []struct {
		name    string
		policy  urlguard.Policy
		servers string
		want    string
	}{
		{"metadata literal", urlguard.HumanConfigured(), "kafka.lan:9092, 169.254.169.254:9092", datasource.FieldNotPermitted},
		{"aws v6 metadata", urlguard.HumanConfigured(), "[fd00:ec2::254]:9092", datasource.FieldNotPermitted},
		{"metadata name", urlguard.HumanConfigured(), "metadata.google.internal:9092", datasource.FieldNotPermitted},
		{"loopback on a shared host", urlguard.PublicOnly(), "127.0.0.1:9092", datasource.FieldNotPermitted},
		{"a url", urlguard.HumanConfigured(), "kafka://broker:9092", datasource.FieldInvalid},
		{"no port number", urlguard.HumanConfigured(), "broker:port", datasource.FieldInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := brokerService(t, tt.policy)

			_, err := svc.Create(context.Background(), kafkaReq(tt.servers))

			assert.Equal(t, datasource.FieldErrors{"bootstrap_servers": tt.want}, err)
		})
	}
}

func TestADestinationFieldNamesTheSourceAfterItsFirstHost(t *testing.T) {
	svc, store, kind := brokerService(t, urlguard.HumanConfigured())
	store.On("CreateDataSource", mock.Anything, mock.MatchedBy(func(ds domain.DataSource) bool { return ds.Name == "b-1.lan:9092" }), mock.Anything).
		Return(domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindKafka, Name: "b-1.lan:9092"}, nil)
	kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil)
	store.On("RecordVerification", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "b-1.lan:9092"}, nil)

	got, err := svc.Create(context.Background(), kafkaReq("B-1.lan:9092,b-2.lan:9092"))

	require.NoError(t, err)
	assert.Equal(t, "b-1.lan:9092", got.Name)
}

func TestMovingADestinationToOtherHostsNeedsTheStoredSecretsAgain(t *testing.T) {
	stored := domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "prod",
		Config: map[string]string{"bootstrap_servers": "b-1.lan:9092,b-2.lan:9092", "username": "ops"}}
	tests := []struct {
		name    string
		patch   domain.PatchDataSourceRequest
		refused bool
	}{
		{"another broker", patch(map[string]string{"bootstrap_servers": "evil.example:9092"}, nil), true},
		{"a broker added", patch(map[string]string{"bootstrap_servers": "b-1.lan:9092,b-2.lan:9092,evil.example:9092"}, nil), true},
		{"the password re-sent", patch(map[string]string{"bootstrap_servers": "evil.example:9092"}, map[string]string{"password": "new"}), false},
		{"the same brokers reordered and recased", patch(map[string]string{"bootstrap_servers": " B-2.lan:9092 , b-1.lan:9092"}, nil), false},
		{"no move", patch(map[string]string{"username": "ops2"}, nil), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, kind := brokerService(t, urlguard.HumanConfigured())
			id := uuid.New()
			ds := stored
			ds.ID = id
			store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
			store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{"password": "s3cret"}, nil)
			if !tt.refused {
				store.On("UpdateDataSource", mock.Anything, mock.Anything, mock.Anything).
					Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
						return ds, nil
					})
				kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil)
				store.On("RecordVerification", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(domain.DataSource{Kind: domain.DataSourceKindKafka}, nil)
			}

			_, err := svc.Update(context.Background(), id, tt.patch)

			if tt.refused {
				assert.Equal(t, datasource.FieldErrors{"secrets": datasource.FieldSecretsRequired}, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
