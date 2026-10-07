package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/suite"
)

type MetricsMiddlewareSuite struct {
	suite.Suite
	metrics *Metrics
	app     *fiber.App
}

func TestMetricsMiddlewareSuite(t *testing.T) {
	suite.Run(t, new(MetricsMiddlewareSuite))
}

func (s *MetricsMiddlewareSuite) SetupTest() {
	s.metrics = &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_requests_total"},
			[]string{"method", "path", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_request_duration_seconds"},
			[]string{"method", "path"}),
	}
	h := &Handler{metrics: s.metrics}
	s.app = fiber.New()
	s.app.Use(h.metricsMiddleware)
	s.app.Get("/v1/tasks/:id", func(c *fiber.Ctx) error { return c.SendString(c.Params("id")) })
}

func (s *MetricsMiddlewareSuite) get(path string) int {
	resp, err := s.app.Test(httptest.NewRequest("GET", path, nil))
	s.Require().NoError(err)
	return resp.StatusCode
}

type series struct {
	labels map[string]string
	count  float64
}

func collect(c prometheus.Collector) []series {
	ch := make(chan prometheus.Metric, 64)
	c.Collect(ch)
	close(ch)
	var out []series
	for m := range ch {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			panic(err)
		}
		labels := map[string]string{}
		for _, l := range pb.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		out = append(out, series{labels: labels, count: pb.GetCounter().GetValue()})
	}
	return out
}

func (s *MetricsMiddlewareSuite) TestRequestsToOneRouteShareOneSeries() {
	for i := 0; i < 3; i++ {
		s.Equal(200, s.get("/v1/tasks/"+uuid.NewString()))
	}

	got := collect(s.metrics.RequestsTotal)
	s.Require().Len(got, 1)
	s.Equal(map[string]string{"method": "GET", "path": "/v1/tasks/:id", "status": "200"}, got[0].labels)
	s.Equal(3.0, got[0].count)
	s.Len(collect(s.metrics.RequestDuration), 1)
}

func (s *MetricsMiddlewareSuite) TestUnmatchedPathsDoNotMintASeriesEach() {
	s.Equal(404, s.get("/nowhere/"+uuid.NewString()))
	s.Equal(404, s.get("/nowhere/"+uuid.NewString()))

	got := collect(s.metrics.RequestsTotal)
	s.Require().Len(got, 1)
	s.Equal(2.0, got[0].count)
	s.Equal("404", got[0].labels["status"])
}
