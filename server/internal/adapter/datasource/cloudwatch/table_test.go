package cloudwatch_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestTableIsEachSeriesLatestDatapointOfTheLast20Minutes(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) {
		return 200, dataPage("m", "ApproximateNumberOfMessagesVisible", [][2]string{{"2026-10-16T11:58:00Z", "12"}, {"2026-10-16T11:50:00Z", "40"}}, "")
	})

	res, err := f.adapter.Table(context.Background(), f.source(nil), nil, domain.TableQuery{Query: backlog})

	require.NoError(t, err)
	form := f.calls[0].form
	assert.Equal(t, "2026-10-16T11:40:00Z", form.Get("StartTime"))
	assert.Equal(t, "2026-10-16T12:00:00Z", form.Get("EndTime"))
	assert.Equal(t, "60", form.Get("MetricDataQueries.member.1.MetricStat.Period"))
	assert.Equal(t, []domain.TableColumn{
		{Key: "QueueName", Label: "QueueName"}, {Key: "stat", Label: "stat"}, {Key: domain.TableValueKey, Label: domain.TableValueKey},
	}, res.Columns)
	assert.Equal(t, []map[string]any{{"QueueName": "orders", "stat": "Maximum", "value": 12.0}}, res.Rows)
}

func TestTableAnswersNoValueForASeriesWithoutDatapoints(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, dataPage("m", "x", nil, "") })

	res, err := f.adapter.Table(context.Background(), f.source(nil), nil, domain.TableQuery{Query: backlog})

	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"QueueName": "orders", "stat": "Maximum", "value": nil}}, res.Rows)
}
