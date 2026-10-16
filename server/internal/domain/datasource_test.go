package domain_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParseHeaderLines(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []domain.HeaderLine
		wantErr bool
	}{
		{"empty", "  \n ", nil, false},
		{"one per line, crlf and blanks", "X-Scope-OrgID: t1\r\n\r\nAuthorization: Bearer a:b", []domain.HeaderLine{
			{Name: "X-Scope-OrgID", Value: "t1"}, {Name: "Authorization", Value: "Bearer a:b"},
		}, false},
		{"no colon", "X-Scope-OrgID t1", nil, true},
		{"space in the name", "X Scope: t1", nil, true},
		{"framing header", "Content-Length: 5", nil, true},
		{"host header", "host: other", nil, true},
		{"gce metadata header", "metadata-flavor: Google", nil, true},
		{"aws metadata token", "X-aws-ec2-metadata-token: abc", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseHeaderLines(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSeriesPointsMarshalAsMillisecondPairsWithNullForNonFinite(t *testing.T) {
	at := time.UnixMilli(1700000000123)
	points := []domain.SeriesPoint{
		domain.FinitePoint(at, 1.5),
		domain.FinitePoint(at, math.NaN()),
		domain.FinitePoint(at, math.Inf(-1)),
	}
	raw, err := json.Marshal(points)
	require.NoError(t, err)
	assert.JSONEq(t, `[[1700000000123,1.5],[1700000000123,null],[1700000000123,null]]`, string(raw))
}

func TestFieldShownFollowsShowWhen(t *testing.T) {
	token := domain.DataSourceField{Key: "token", Type: domain.FieldSecret, ShowWhen: map[string]string{"auth": "bearer"}}
	assert.True(t, token.Shown(map[string]string{"auth": "bearer"}))
	assert.False(t, token.Shown(map[string]string{"auth": "basic"}))
	assert.False(t, token.Shown(nil))
	assert.True(t, domain.DataSourceField{Key: "url"}.Shown(nil))
	assert.True(t, token.IsSecret())
	assert.True(t, domain.DataSourceField{Type: domain.FieldHeaders}.IsSecret())
}
