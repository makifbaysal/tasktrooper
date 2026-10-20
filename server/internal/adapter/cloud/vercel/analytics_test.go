package vercel

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestProjectAnalyticsReadsWhichFeaturesAreSwitchedOn(t *testing.T) {
	cases := []struct {
		name string
		json string
		want domain.CloudAnalytics
	}{
		{"never switched on", `{"webAnalytics":{"id":"a"},"speedInsights":{"id":"s","hasData":false}}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsNotEnabled}},
		{"no fields at all", `{}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsNotEnabled}},
		{"web analytics on", `{"webAnalytics":{"id":"a","enabledAt":1783521344822,"hasData":true},"speedInsights":{"id":"s"}}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsEnabled, SpeedInsights: domain.AnalyticsNotEnabled}},
		{"speed insights switched off again", `{"webAnalytics":{"enabledAt":2},"speedInsights":{"enabledAt":1783549664004,"disabledAt":1789516800000}}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsEnabled, SpeedInsights: domain.AnalyticsNotEnabled}},
		{"switched on again after being off", `{"speedInsights":{"enabledAt":9,"disabledAt":5}}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsEnabled}},
		{"canceled", `{"speedInsights":{"enabledAt":1776853428961,"disabledAt":1783549653106,"canceledAt":1783549653106}}`,
			domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsNotEnabled}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectAnalytics([]byte(tc.json))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAnalyticsAsksTheProjectInItsTeam(t *testing.T) {
	var path, team string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, team = r.URL.Path, r.URL.Query().Get("teamId")
		_, _ = w.Write([]byte(`{"webAnalytics":{"enabledAt":1},"speedInsights":{"id":"s"}}`))
	}))
	got, err := NewProvider(c).Analytics(context.Background(),
		domain.CloudCredential{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t", "team_id": "team_1"}},
		domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"})
	require.NoError(t, err)
	assert.Equal(t, "/v9/projects/prj_1", path)
	assert.Equal(t, "team_1", team)
	assert.Equal(t, domain.CloudAnalytics{WebAnalytics: domain.AnalyticsEnabled, SpeedInsights: domain.AnalyticsNotEnabled}, got)
}
