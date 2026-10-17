package rabbitmq_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/rabbitmq"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type broker struct {
	mu       sync.Mutex
	requests []*http.Request
	routes   map[string]http.HandlerFunc
}

// serve routes by the escaped path, which is how the management API tells
// the vhost "/" (%2F) from a path separator.
func serve(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *broker) {
	t.Helper()
	b := &broker{routes: routes}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.requests = append(b.requests, r)
		h, ok := b.routes[r.URL.EscapedPath()]
		b.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":"Object Not Found","reason":"Not Found"}`, http.StatusNotFound)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, b
}

func (b *broker) last() *http.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests[len(b.requests)-1]
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func newAdapter() *rabbitmq.Adapter {
	a := rabbitmq.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(url string, config map[string]string) domain.DataSource {
	c := map[string]string{"url": url, "username": "monitor"}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindRabbitMQ, Config: c}
}

var secrets = map[string]string{"password": "s3cret-pass"}

func instant(query string) domain.SeriesQuery {
	now := time.Now()
	return domain.SeriesQuery{Query: query, From: now.Add(-time.Hour), To: now, Step: time.Minute}
}

const ordersQueue = `{"name": "orders", "state": "running", "messages": 12, "messages_ready": 10, "messages_unacknowledged": 2, "consumers": 3,
	"message_stats": {"publish_details": {"rate": 4.5}, "ack_details": {"rate": 4.0}}}`

func TestVerifyReadsTheOverviewAndChecksTheVhost(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{
		"/rmq/api/overview":     reply(200, `{"rabbitmq_version": "4.1.0", "cluster_name": "rabbit@prod"}`),
		"/rmq/api/vhosts/%2F":   reply(200, `{"name": "/"}`),
		"/rmq/api/vhosts/sales": reply(200, `{"name": "sales"}`),
	})
	a := newAdapter()

	info, err := a.Verify(context.Background(), source(srv.URL+"/rmq", nil), secrets)

	require.NoError(t, err)
	assert.Equal(t, domain.DataSourceInfo{Version: "4.1.0", Detail: "RabbitMQ · rabbit@prod · vhost /"}, info)
	user, pass, ok := b.last().BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "monitor", user)
	assert.Equal(t, "s3cret-pass", pass)

	_, err = a.Verify(context.Background(), source(srv.URL+"/rmq", map[string]string{"vhost": "sales"}), secrets)
	require.NoError(t, err)
	_, err = a.Verify(context.Background(), source(srv.URL+"/rmq", map[string]string{"vhost": "nope"}), secrets)
	assert.ErrorContains(t, err, `the virtual host "nope" does not exist`)
}

func TestVerifyTellsAnotherServiceApart(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/overview": func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html>Grafana</html>")
	}})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), secrets)

	assert.ErrorContains(t, err, "not as the RabbitMQ management API")
}

func TestSeriesReadsOneQueueAsALivePoint(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/queues/%2F/orders": reply(200, ordersQueue)})
	a := newAdapter()
	tests := []struct {
		metric string
		want   float64
	}{
		{"", 12}, {"ready", 10}, {"unacked", 2}, {"consumers", 3},
		{"publish_rate", 4.5}, {"ack_rate", 4.0}, {"deliver_rate", 0},
	}
	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			q := instant(fmt.Sprintf(`{"queue": "orders", "metric": %q}`, tt.metric))
			if tt.metric == "" {
				q = instant(`{"queue": "orders"}`)
			}
			res, err := a.Series(context.Background(), source(srv.URL, nil), secrets, q)
			require.NoError(t, err)
			require.Len(t, res.Series, 1)
			s := res.Series[0]
			assert.Equal(t, map[string]string{"queue": "orders", "vhost": "/"}, s.Labels)
			require.Len(t, s.Points, 1)
			require.NotNil(t, s.Points[0].Value)
			assert.Equal(t, tt.want, *s.Points[0].Value)
			assert.WithinDuration(t, q.To, s.Points[0].Time, time.Second)
		})
	}
}

func TestAPatternReadsEveryMatchingQueueAcrossPages(t *testing.T) {
	var asked []string
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/queues/sales": func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = fmt.Fprint(w, `{"page": 1, "page_count": 2, "filtered_count": 3, "items": [`+ordersQueue+`, {"name": "invoices", "messages": 1}]}`)
		default:
			_, _ = fmt.Fprint(w, `{"page": 2, "page_count": 2, "filtered_count": 3, "items": [{"name": "orders.dlq", "state": "idle", "messages": 7, "messages_ready": 7, "messages_unacknowledged": 0, "consumers": 0}]}`)
		}
	}})
	ds := source(srv.URL, map[string]string{"vhost": "sales"})

	res, err := newAdapter().Table(context.Background(), ds, secrets, domain.TableQuery{Query: `{"queue": "orders*"}`, At: time.Now()})

	require.NoError(t, err)
	require.Len(t, asked, 2)
	first := asked[0]
	assert.Contains(t, first, "use_regex=true")
	assert.Contains(t, first, "name="+`%5Eorders.%2A%24`)
	assert.Contains(t, first, "page_size=500")
	require.Len(t, res.Rows, 2, "invoices does not match even when the server answers it")
	assert.Equal(t, map[string]any{
		"queue": "orders", "state": "running", "ready": 10.0, "unacked": 2.0, "consumers": 3.0,
		"publish_rate": 4.5, "deliver_rate": 0.0, "ack_rate": 4.0,
	}, res.Rows[0])
	assert.Equal(t, "orders.dlq", res.Rows[1]["queue"])
	assert.False(t, res.Truncated)

	a := newAdapter()
	a.SetMaxQueues(1)
	res, err = a.Table(context.Background(), ds, secrets, domain.TableQuery{Query: `{"queue": "orders*"}`, At: time.Now()})
	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
	assert.True(t, res.Truncated)

	series, err := newAdapter().Series(context.Background(), ds, secrets, instant(`{"queue": "orders*", "metric": "ready"}`))
	require.NoError(t, err)
	require.Len(t, series.Series, 2)
	assert.Equal(t, "orders.dlq", series.Series[1].Labels["queue"])
	assert.Equal(t, 7.0, *series.Series[1].Points[0].Value)
}

func TestAMissingQueueAnswersNoSeriesAndSaysWhy(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{})

	res, err := newAdapter().Series(context.Background(), source(srv.URL, nil), secrets, instant(`{"queue": "gone"}`))

	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Equal(t, []string{`no queue matches "gone" in the virtual host "/"`}, res.Warnings)
}

func TestAWindowThatEndedEarlierIsNotAnsweredWithTheLiveValue(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{"/api/queues/%2F/orders": reply(200, ordersQueue)})
	q := instant(`{"queue": "orders"}`)
	q.From, q.To = q.From.Add(-2*time.Hour), q.To.Add(-time.Hour)

	res, err := newAdapter().Series(context.Background(), source(srv.URL, nil), secrets, q)

	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Len(t, res.Warnings, 1)
	assert.Empty(t, b.requests)
}

func TestRefusalsNeverCarryThePasswordOrItsEncoding(t *testing.T) {
	echo := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error": "not_authorised", "reason": "Login failed for %s (s3cret-pass)"}`, r.Header.Get("Authorization"))
	}
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/overview": echo})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), secrets)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 401)")
	assert.NotContains(t, err.Error(), "s3cret-pass")
	assert.NotContains(t, err.Error(), base64.StdEncoding.EncodeToString([]byte("monitor:s3cret-pass")))
}

func TestTheURLPolicyJudgesTheManagementURL(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{"/api/overview": reply(200, `{"rabbitmq_version": "4.1.0"}`)})

	for _, raw := range []string{"http://169.254.169.254", "http://[fd00:ec2::254]:15672", "http://metadata.google.internal"} {
		_, err := newAdapter().Verify(context.Background(), source(raw, nil), secrets)
		assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%s: %v", raw, err)
	}
	shared := rabbitmq.New()
	shared.SetURLPolicy(urlguard.PublicOnly())
	_, err := shared.Verify(context.Background(), source(srv.URL, nil), secrets)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	_, err = rabbitmq.New().Verify(context.Background(), source(srv.URL, nil), secrets)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, b.requests)
}

func TestARedirectToAnotherHostIsRefused(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{"/api/overview": func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:9/api/overview", http.StatusFound)
	}})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), secrets)

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Len(t, b.requests, 1)
}

func TestDiscoverListsTheVhostsQueues(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{
		"/api/queues/%2F": reply(200, `{"page": 1, "page_count": 1, "items": [{"name": "orders"}, {"name": "emails"}]}`),
	})

	inv, err := newAdapter().Discover(context.Background(), source(srv.URL, nil), secrets)

	require.NoError(t, err)
	assert.Equal(t, domain.Inventory{Metrics: []string{"queue/emails", "queue/orders"}}, inv)
	assert.Equal(t, "name", b.last().URL.Query().Get("columns"))
}

func TestAQueryThatIsNotRabbitMQJSONIsTheCallersMistake(t *testing.T) {
	for _, q := range []string{`rabbitmq_queue_messages_ready`, `{"metric": "ready"}`, `{"queue": "q", "metric": "lag"}`, `{"queue": "q", "vhost": "/"}`} {
		_, err := newAdapter().Series(context.Background(), source("http://127.0.0.1:1", nil), secrets, instant(q))
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
	}
}

func TestSchemaMarksOnlyThePasswordSecret(t *testing.T) {
	secret := map[string]bool{}
	for _, f := range newAdapter().Schema().Fields {
		secret[f.Key] = f.IsSecret()
	}
	assert.Equal(t, map[string]bool{"url": false, "username": false, "vhost": false, "password": true}, secret)
}

func TestDiscoverQueuesOffersEachQueueByItsNameButNotOneAQueryWouldReadAsAPattern(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/queues/%2F": reply(200, `{"page": 1, "page_count": 1, "items": [{"name": "orders"}, {"name": "orders*"}]}`),
	})

	targets, err := newAdapter().DiscoverQueues(context.Background(), source(srv.URL, nil), secrets)

	require.NoError(t, err)
	assert.Equal(t, []domain.QueueTarget{{Name: "orders", Params: map[string]string{"queue": "orders"}}}, targets)
}

func TestAListingStopsAtItsPageCap(t *testing.T) {
	pages := 0
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/queues/%2F": func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"page_count": 100000, "items": [{"name": "other"}]}`)
	}})

	res, err := newAdapter().Table(context.Background(), source(srv.URL, nil), secrets, domain.TableQuery{Query: `{"queue": "orders*"}`, At: time.Now()})

	require.NoError(t, err)
	assert.Empty(t, res.Rows)
	assert.True(t, res.Truncated)
	assert.Equal(t, 40, pages, "a server claiming endless pages of queues the pattern drops is not read forever")
}

func TestADotSegmentIsNeverSentAsAPath(t *testing.T) {
	srv, b := serve(t, map[string]http.HandlerFunc{})

	for _, name := range []string{"..", "."} {
		_, err := newAdapter().Series(context.Background(), source(srv.URL, nil), secrets, instant(`{"queue": "`+name+`"}`))
		require.Error(t, err)
	}
	assert.Empty(t, b.requests)
}
