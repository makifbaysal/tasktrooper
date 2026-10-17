package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBrokerQueriesDefaultToTheBacklogMeasure(t *testing.T) {
	k, err := domain.ParseKafkaQuery(`{"group": "billing"}`)
	require.NoError(t, err)
	assert.Equal(t, domain.KafkaQuery{Group: "billing", Metric: domain.KafkaMetricLag}, k)

	r, err := domain.ParseRabbitMQQuery(`{"queue": "orders.*"}`)
	require.NoError(t, err)
	assert.Equal(t, domain.RabbitMQQuery{Queue: "orders.*", Metric: domain.RabbitMQMetricMessages}, r)

	n, err := domain.ParseNATSQuery(`{"stream": "ORDERS", "consumer": "billing", "metric": "ack_pending"}`)
	require.NoError(t, err)
	assert.Equal(t, domain.NATSQuery{Stream: "ORDERS", Consumer: "billing", Metric: domain.NATSMetricAckPending}, n)
}

func TestBrokerQueriesRefuseWhatTheyCannotRead(t *testing.T) {
	tests := []struct {
		name  string
		parse func(string) error
		raw   string
		want  string
	}{
		{"kafka without a group", kafka, `{"topic": "t"}`, "group is required"},
		{"kafka unknown key", kafka, `{"group": "g", "partition": 1}`, "unknown field"},
		{"kafka unknown metric", kafka, `{"group": "g", "metric": "rate"}`, "not one of"},
		{"kafka blank topic", kafka, `{"group": "g", "topic": "  "}`, "topic is blank"},
		{"kafka two values", kafka, `{"group": "g"} {"group": "h"}`, "more than one JSON value"},
		{"kafka not json", kafka, `sum(lag)`, "not a Kafka query"},
		{"rabbitmq without a queue", rabbit, `{"metric": "ready"}`, "queue is required"},
		{"rabbitmq control character", rabbit, `{"queue": "a\nb"}`, "control character"},
		{"rabbitmq overlong", rabbit, `{"queue": "` + strings.Repeat("q", 1025) + `"}`, "longer than"},
		{"nats without a stream", nats, `{"consumer": "c"}`, "stream is required"},
		{"nats unknown metric", nats, `{"stream": "S", "metric": "lag"}`, "not one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, tt.parse(tt.raw), tt.want)
		})
	}
}

func kafka(raw string) error {
	_, err := domain.ParseKafkaQuery(raw)
	return err
}

func rabbit(raw string) error {
	_, err := domain.ParseRabbitMQQuery(raw)
	return err
}

func nats(raw string) error {
	_, err := domain.ParseNATSQuery(raw)
	return err
}

func TestParseHostList(t *testing.T) {
	got, err := domain.ParseHostList(" Kafka-1.lan:9093, kafka-2.lan ,,[::1]:9094, ::1, [fd12::1], 10.0.0.5, kafka-2.lan:9092", "9092")
	require.NoError(t, err)
	assert.Equal(t, []string{"kafka-1.lan:9093", "kafka-2.lan:9092", "[::1]:9094", "[::1]:9092", "[fd12::1]:9092", "10.0.0.5:9092"}, got)

	got, err = domain.ParseHostList("b-1.lan, b-2.lan:9096, ::1", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"b-1.lan", "b-2.lan:9096", "::1"}, got)

	for _, bad := range []string{"", " , ", "kafka://b:9092", "user@b:9092", "b:0", "b:99999", "b:port", "b:", "[b]:9092", "b c:9092"} {
		_, err := domain.ParseHostList(bad, "9092")
		assert.Error(t, err, bad)
	}
}

func TestBrokerTargetLeavesTheMeasureOut(t *testing.T) {
	lag, ok := domain.BrokerTarget(domain.DataSourceKindKafka, `{"group": "billing", "metric": "lag"}`)
	assert.True(t, ok)
	consumers, _ := domain.BrokerTarget(domain.DataSourceKindKafka, `{"metric":"consumers","group":"billing"}`)
	assert.Equal(t, lag, consumers, "one group's table whatever is measured")
	topic, _ := domain.BrokerTarget(domain.DataSourceKindKafka, `{"group": "billing", "topic": "orders"}`)
	assert.NotEqual(t, lag, topic)

	q1, _ := domain.BrokerTarget(domain.DataSourceKindRabbitMQ, `{"queue": "orders", "metric": "ready"}`)
	q2, _ := domain.BrokerTarget(domain.DataSourceKindRabbitMQ, `{"queue": "orders"}`)
	assert.Equal(t, q1, q2)
	n1, _ := domain.BrokerTarget(domain.DataSourceKindNATS, `{"stream": "S", "consumer": "c", "metric": "waiting"}`)
	n2, _ := domain.BrokerTarget(domain.DataSourceKindNATS, `{"stream": "S", "consumer": "c"}`)
	assert.Equal(t, n1, n2)

	_, ok = domain.BrokerTarget(domain.DataSourceKindKafka, `not json`)
	assert.False(t, ok)
	_, ok = domain.BrokerTarget(domain.DataSourceKindPrometheus, `up`)
	assert.False(t, ok)
}
