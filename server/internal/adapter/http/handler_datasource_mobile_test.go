package http

import (
	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/appstoremetrics"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/playvitals"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/sentry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func (s *DataSourceHandlerSuite) TestStoreAndSentryKindsListTheirFormSchema() {
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:            s.store,
		StoreCredentials: &mocks.StoreCredentialReader{},
		Kinds:            []port.DataSourceKindAdapter{playvitals.New(), appstoremetrics.New(), sentry.New()},
		URLPolicy:        &policy,
	})
	s.Require().NoError(err)
	s.app = fiber.New(fiber.Config{JSONEncoder: goccyjson.Marshal, JSONDecoder: goccyjson.Unmarshal})
	(&Handler{dataSourceSvc: svc}).registerDataSourceRoutes(s.app)

	status, out := s.do("GET", "/v1/data-source-kinds", "")
	s.Equal(200, status)
	kinds := out["kinds"].([]any)
	s.Require().Len(kinds, 3)
	for i, want := range []struct {
		kind, provider string
		capabilities   []any
	}{
		{"google_play_vitals", "google_play", []any{"metrics", "crashes"}},
		{"app_store_metrics", "asc", []any{"metrics", "crashes"}},
	} {
		k := kinds[i].(map[string]any)
		s.Equal(want.kind, k["kind"])
		s.Equal(want.capabilities, k["capabilities"])
		cred := k["fields"].([]any)[0].(map[string]any)
		s.Equal("store_credential", cred["key"])
		s.Equal("store_credential", cred["type"])
		s.Equal(want.provider, cred["provider"])
		s.Equal(want.provider, cred["default"])
		s.Equal(false, cred["secret"])
	}
	sentryKind := kinds[2].(map[string]any)
	s.Equal([]any{"metrics", "errors", "crashes"}, sentryKind["capabilities"])
	fields := sentryKind["fields"].([]any)
	s.Equal("https://sentry.io", fields[0].(map[string]any)["default"])
	s.Equal(true, fields[2].(map[string]any)["secret"])
}
