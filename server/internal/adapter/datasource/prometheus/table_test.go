package prometheus_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestTableIsAnInstantQueryWhoseSamplesAreRows(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query": jsonReply(200, `{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"topic":"orders","partition":"10"},"value":[1760702400,"7"]},
			{"metric":{"__name__":"kafka_consumergroup_lag","topic":"orders","partition":"2"},"value":[1760702400,"NaN"]},
			{"metric":{"topic":"audit","partition":"0"},"value":[1760702400,"0"]}]}}`),
	})
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	res, err := newAdapter().Table(context.Background(), source(srv.URL, nil), nil,
		domain.TableQuery{Query: `sum by (topic, partition) (kafka_consumergroup_lag{consumergroup="billing"})`, At: at})

	require.NoError(t, err)
	assert.Equal(t, []domain.TableColumn{
		{Key: "partition", Label: "partition"}, {Key: "topic", Label: "topic"}, {Key: domain.TableValueKey, Label: domain.TableValueKey},
	}, res.Columns)
	assert.Equal(t, []map[string]any{
		{"topic": "audit", "partition": "0", "value": 0.0},
		{"topic": "orders", "partition": "10", "value": 7.0},
		{"topic": "orders", "partition": "2", "value": nil},
	}, res.Rows)
	req, form := rec.last()
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, []string{`sum by (topic, partition) (kafka_consumergroup_lag{consumergroup="billing"})`}, form["query"])
	assert.Equal(t, []string{"1792238400.000"}, form["time"])
}

func TestTableReadsAScalarAsOneRow(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query": jsonReply(200, `{"status":"success","data":{"resultType":"scalar","result":[1760702400,"3"]}}`),
	})

	res, err := newAdapter().Table(context.Background(), source(srv.URL, nil), nil, domain.TableQuery{Query: "3"})

	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"value": 3.0}}, res.Rows)
}

func TestTableRefusesARangeVector(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query": jsonReply(200, `{"status":"success","data":{"resultType":"matrix","result":[]}}`),
	})

	_, err := newAdapter().Table(context.Background(), source(srv.URL, nil), nil, domain.TableQuery{Query: "up[5m]"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery))
}
