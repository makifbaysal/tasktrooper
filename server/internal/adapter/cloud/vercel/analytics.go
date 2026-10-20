package vercel

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.CloudAnalyticsReader = (*Provider)(nil)

// feature is how a project JSON carries Web Analytics and Speed Insights: the
// object exists on every project, and only enabledAt later than disabledAt
// (and any cancellation) means it is on.
type feature struct {
	EnabledAt  int64 `json:"enabledAt"`
	DisabledAt int64 `json:"disabledAt"`
	CanceledAt int64 `json:"canceledAt"`
}

func (f *feature) state() domain.AnalyticsState {
	if f == nil || f.EnabledAt == 0 || f.DisabledAt >= f.EnabledAt || f.CanceledAt >= f.EnabledAt {
		return domain.AnalyticsNotEnabled
	}
	return domain.AnalyticsEnabled
}

// ProjectAnalytics reads which of the two a project's JSON says are on.
func ProjectAnalytics(project []byte) (domain.CloudAnalytics, error) {
	var raw struct {
		WebAnalytics  *feature `json:"webAnalytics"`
		SpeedInsights *feature `json:"speedInsights"`
	}
	if err := json.Unmarshal(project, &raw); err != nil {
		return domain.CloudAnalytics{}, err
	}
	return domain.CloudAnalytics{WebAnalytics: raw.WebAnalytics.state(), SpeedInsights: raw.SpeedInsights.state()}, nil
}

func (p *Provider) Analytics(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudAnalytics, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.CloudAnalytics{}, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}
	var raw json.RawMessage
	if err := p.client.getJSON(ctx, token, "/v9/projects/"+url.PathEscape(ref.ID), withTeam(nil, teamID), &raw); err != nil {
		return domain.CloudAnalytics{}, wrapVercelErr(err)
	}
	out, err := ProjectAnalytics(raw)
	if err != nil {
		return domain.CloudAnalytics{}, wrapVercelErr(err)
	}
	return out, nil
}
