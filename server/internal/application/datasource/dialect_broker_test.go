package datasource_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestValidateRecipeAdmitsBrokerPlaceholdersOnlyInTheEntityName(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(kind domain.DataSourceKind, query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Params: []string{"group", "topic", "queue", "stream", "consumer"},
			Signals: map[string]string{"queue.backlog": query}}
	}

	for _, ok := range []struct {
		kind  domain.DataSourceKind
		query string
	}{
		{domain.DataSourceKindKafka, `{"group": "$group", "topic": "$topic", "metric": "lag"}`},
		{domain.DataSourceKindKafka, `{"group": "prod-$group"}`},
		{domain.DataSourceKindRabbitMQ, `{"queue": "$queue.*", "metric": "ready"}`},
		{domain.DataSourceKindNATS, `{"stream": "$stream", "consumer": "$consumer", "metric": "ack_pending"}`},
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok.kind, ok.query)), ok.query)
	}

	cases := []struct {
		name  string
		kind  domain.DataSourceKind
		query string
		want  string
	}{
		{"bare", domain.DataSourceKindKafka, `{"group": $group}`, "$group must sit inside a JSON string"},
		{"in the metric", domain.DataSourceKindKafka, `{"group": "g", "metric": "$topic"}`, "may appear only in the group or topic value"},
		{"in a key", domain.DataSourceKindRabbitMQ, `{"$queue": "q"}`, "may not sit in a JSON key"},
		{"a built-in", domain.DataSourceKindNATS, `{"stream": "S$__rate"}`, "has no meaning in a NATS query"},
		{"the quantile", domain.DataSourceKindRabbitMQ, `{"queue": "$__quantile"}`, "has no meaning in a RabbitMQ query"},
		{"undeclared", domain.DataSourceKindKafka, `{"group": "$vhost"}`, "uses $vhost"},
		{"not JSON", domain.DataSourceKindKafka, `sum(kafka_consumergroup_lag)`, "is not JSON"},
		{"unknown key", domain.DataSourceKindRabbitMQ, `{"queue": "q", "vhost": "/"}`, "unknown field"},
		{"unknown metric", domain.DataSourceKindNATS, `{"stream": "S", "metric": "lag"}`, "not one of"},
		{"no entity", domain.DataSourceKindKafka, `{"metric": "lag"}`, "group is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.kind, tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestBrokerParamsAreEscapedForAJSONString(t *testing.T) {
	got, err := datasource.RenderKindQuery(domain.DataSourceKindKafka, `{"group": "$group", "metric": "lag"}`,
		map[string]string{"group": `a", "metric": "members`}, datasource.RenderOptions{Step: time.Minute})

	require.NoError(t, err)
	q, err := domain.ParseKafkaQuery(got)
	require.NoError(t, err)
	assert.Equal(t, domain.KafkaQuery{Group: `a", "metric": "members`, Metric: domain.KafkaMetricLag}, q)
}

func TestABrokerDetectMetricIsAnInventoryEntry(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(kind domain.DataSourceKind, metric, query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Detect: domain.RecipeDetect{Metric: metric},
			Signals: map[string]string{"queue.backlog": query}}
	}
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindKafka, "group/billing", `{"group": "billing"}`)))
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindNATS, "consumer/ORDERS/billing", `{"stream": "ORDERS"}`)))
	assert.ErrorContains(t, c.ValidateRecipe(recipe(domain.DataSourceKindRabbitMQ, "rabbitmq_queue_messages", `{"queue": "q"}`)), "detect.metric")
}
