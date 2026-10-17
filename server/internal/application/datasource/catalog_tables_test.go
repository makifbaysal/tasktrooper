package datasource_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestEveryBrokerRecipeHasAQueuesTableThatRenders(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)

	withTable := map[string]bool{}
	for _, r := range c.Recipes() {
		table, ok := r.Tables[domain.RecipeTableQueues]
		if !ok {
			continue
		}
		withTable[r.ID] = true
		params := map[string]string{}
		for _, p := range r.Params {
			params[p] = `sam"ple`
		}
		assert.NotEmpty(t, table.KeyLabels, r.ID)
		for _, col := range table.ValueColumns() {
			rendered, err := datasource.RenderKindQuery(r.SourceKind, table.Queries[col.Key], params, datasource.RenderOptions{Step: time.Minute})
			require.NoError(t, err, "%s/%s", r.ID, col.Key)
			assert.NotContains(t, rendered, "$", "%s/%s", r.ID, col.Key)
			if r.SourceKind == domain.DataSourceKindCloudWatch {
				q, err := domain.ParseCloudWatchQuery(rendered)
				require.NoError(t, err, "%s/%s", r.ID, col.Key)
				assert.Empty(t, q.Expression, "%s/%s: a CloudWatch table reads one metric, whose dimensions are its key labels", r.ID, col.Key)
				for _, k := range table.KeyLabels {
					assert.Contains(t, q.Dimensions, k, "%s/%s", r.ID, col.Key)
				}
			}
		}
	}
	for _, id := range []string{
		"kafka-exporter", "kminion", "rabbitmq-prometheus", "gcp-pubsub", "aws-sqs", "aws-msk",
		"nats-exporter", "redis-exporter", "redis-exporter-lists",
	} {
		assert.True(t, withTable[id], id)
	}
}

func TestATableWithOneValueColumnTakesTheShortQueryForm(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{
		"signals.yaml": {Data: []byte(testSignals)},
		"recipes/r.yaml": {Data: []byte(`id: r
source_kind: prometheus
params: [group]
signals: {queue.backlog: 'sum(lag{group="$group"})'}
tables:
  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag, unit: messages}]
    query: 'sum by (topic) (lag{group="$group"})'
`)},
	}, prometheusOnly)
	require.NoError(t, err)

	r := c.Recipes()[0]
	assert.Equal(t, domain.RecipeTable{
		Columns:   []domain.TableColumn{{Key: "topic", Label: "Topic"}, {Key: "lag", Label: "Lag", Unit: "messages"}},
		KeyLabels: []string{"topic"},
		Queries:   map[string]string{"lag": `sum by (topic) (lag{group="$group"})`},
	}, r.Tables[domain.RecipeTableQueues])
	assert.Equal(t, []string{"group"}, datasource.TableParams(r, domain.RecipeTableQueues))
}

func TestLoadCatalogRefusesABrokenTable(t *testing.T) {
	const head = `id: r
source_kind: prometheus
params: [group]
signals: {queue.backlog: 'sum(lag{group="$group"})'}
tables:
`
	cases := []struct {
		name, table, want string
	}{
		{"unknown table", `  partitions:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    query: 'sum by (topic) (lag)'`, "tables.partitions is not a known table"},
		{"query and queries", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    query: 'x'
    queries: {lag: 'x'}`, "not both"},
		{"query for two value columns", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}, {key: rate, label: Rate}]
    query: 'x'`, "use queries"},
		{"no key labels", `  queues:
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    queries: {lag: 'x'}`, "has no key_labels"},
		{"key label not a column", `  queues:
    key_labels: [partition]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    queries: {lag: 'x', topic: 'y'}`, `key label "partition" is not a column`},
		{"value column without a query", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}, {key: rate, label: Rate}]
    queries: {lag: 'x'}`, `column "rate" has no query`},
		{"query for a key column", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    queries: {lag: 'x', topic: 'y'}`, "queries.topic is not a value column"},
		{"column listed twice", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}, {key: lag, label: Lag}]
    queries: {lag: 'x'}`, `column "lag" is listed twice`},
		{"bad column key", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: "consumer group", label: Lag}]
    queries: {"consumer group": 'x'}`, "must be letters, digits and underscores"},
		{"column without a label", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag}]
    queries: {lag: 'x'}`, `column "lag" has no label`},
		{"undeclared param", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    query: 'sum by (topic) (lag{group="$vhost"})'`, `table "queues" column "lag" uses $vhost`},
		{"param in a regex matcher", `  queues:
    key_labels: [topic]
    columns: [{key: topic, label: Topic}, {key: lag, label: Lag}]
    query: 'sum by (topic) (lag{group=~"$group"})'`, `table "queues" column "lag": $group must sit inside a double-quoted label value`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"signals.yaml":   {Data: []byte(testSignals)},
				"recipes/r.yaml": {Data: []byte(head + tc.table + "\n")},
			}
			_, err := datasource.LoadCatalog(fsys, prometheusOnly)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestACloudWatchTableQueryKeepsItsParamsInDimensionValues(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindCloudWatch, Params: []string{"queue"},
			Signals: map[string]string{"queue.backlog": `{"namespace": "AWS/SQS", "metric": "M", "stat": "Sum", "dimensions": {"QueueName": "$queue"}}`},
			Tables: map[string]domain.RecipeTable{domain.RecipeTableQueues: {
				Columns:   []domain.TableColumn{{Key: "QueueName", Label: "Queue"}, {Key: "visible", Label: "Visible"}},
				KeyLabels: []string{"QueueName"},
				Queries:   map[string]string{"visible": query},
			}}}
	}

	assert.NoError(t, c.ValidateRecipe(recipe(`{"namespace": "AWS/SQS", "metric": "V", "stat": "Maximum", "dimensions": {"QueueName": "$queue"}}`)))
	assert.ErrorContains(t, c.ValidateRecipe(recipe(`{"namespace": "$queue", "metric": "V", "stat": "Maximum"}`)),
		`table "queues" column "visible": $queue may appear only in a dimension value`)
}
