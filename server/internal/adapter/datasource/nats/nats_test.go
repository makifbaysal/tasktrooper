package nats_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/nats"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type recorded struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (r *recorded) last() *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1]
}

func serve(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.requests = append(rec.requests, r)
			rec.mu.Unlock()
			h(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func newAdapter() *nats.Adapter {
	a := nats.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(url string) domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindNATS, Config: map[string]string{"url": url}}
}

func instant(query string) domain.SeriesQuery {
	now := time.Now()
	return domain.SeriesQuery{Query: query, From: now.Add(-time.Hour), To: now, Step: time.Minute}
}

const jsz = `{"server_id": "NA1", "streams": 2, "account_details": [
	{"name": "$G", "stream_detail": [
		{"name": "ORDERS", "state": {"messages": 40}, "consumer_detail": [
			{"stream_name": "ORDERS", "name": "billing", "num_pending": 12, "num_ack_pending": 3, "num_redelivered": 1, "num_waiting": 2},
			{"stream_name": "ORDERS", "name": "audit", "num_pending": 0, "num_ack_pending": 0, "num_redelivered": 0, "num_waiting": 1}
		]},
		{"name": "EMAILS", "consumer_detail": [{"name": "sender", "num_pending": 5}]}
	]}
]}`

func TestVerifyReadsVarz(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/varz": reply(200, `{"server_id": "NA1", "server_name": "nats-1", "version": "2.11.4", "jetstream": {"config": {"max_memory": 1}}}`),
	})

	info, err := newAdapter().Verify(context.Background(), source(srv.URL), map[string]string{"token": "s3cret-token"})

	require.NoError(t, err)
	assert.Equal(t, domain.DataSourceInfo{Version: "2.11.4", Detail: "NATS · nats-1"}, info)
	assert.Equal(t, "Bearer s3cret-token", rec.last().Header.Get("Authorization"))
}

func TestVerifySaysWhenJetStreamIsOffAndTellsAnotherServiceApart(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/varz": reply(200, `{"server_id": "NA1", "server_name": "nats-1", "version": "2.11.4"}`)})
	info, err := newAdapter().Verify(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, "NATS · nats-1 · JetStream off", info.Detail)

	other, _ := serve(t, map[string]http.HandlerFunc{"/varz": reply(200, `{"status": "ok"}`)})
	_, err = newAdapter().Verify(context.Background(), source(other.URL), nil)
	assert.ErrorContains(t, err, "not as a NATS monitoring endpoint")
}

func TestSeriesReadsEachConsumerOfTheStream(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/jsz": reply(200, jsz)})
	a := newAdapter()

	q := instant(`{"stream": "ORDERS"}`)
	res, err := a.Series(context.Background(), source(srv.URL), nil, q)

	require.NoError(t, err)
	query := rec.last().URL.Query()
	assert.Equal(t, "true", query.Get("consumers"))
	assert.Equal(t, "true", query.Get("streams"))
	require.Len(t, res.Series, 2)
	assert.Equal(t, "pending", res.Series[0].Name)
	assert.Equal(t, map[string]string{"account": "$G", "stream": "ORDERS", "consumer": "audit"}, res.Series[0].Labels)
	assert.Equal(t, "billing", res.Series[1].Labels["consumer"])
	assert.Equal(t, 12.0, *res.Series[1].Points[0].Value)
	assert.WithinDuration(t, q.To, res.Series[1].Points[0].Time, time.Second)

	for metric, want := range map[string]float64{"ack_pending": 3, "redelivered": 1, "waiting": 2, "pending": 12} {
		res, err := a.Series(context.Background(), source(srv.URL), nil, instant(fmt.Sprintf(`{"stream": "ORDERS", "consumer": "billing", "metric": %q}`, metric)))
		require.NoError(t, err)
		require.Len(t, res.Series, 1, metric)
		assert.Equal(t, want, *res.Series[0].Points[0].Value, metric)
	}
}

func TestAMissingStreamOrConsumerSaysWhy(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/jsz": reply(200, jsz)})

	res, err := newAdapter().Series(context.Background(), source(srv.URL), nil, instant(`{"stream": "NOPE"}`))
	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Equal(t, []string{`this server holds no stream "NOPE"`}, res.Warnings)

	res, err = newAdapter().Series(context.Background(), source(srv.URL), nil, instant(`{"stream": "ORDERS", "consumer": "nope"}`))
	require.NoError(t, err)
	assert.Equal(t, []string{`the stream "ORDERS" has no consumer "nope"`}, res.Warnings)
}

func TestTableAnswersOneRowPerConsumer(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/jsz": reply(200, jsz)})

	res, err := newAdapter().Table(context.Background(), source(srv.URL), nil, domain.TableQuery{Query: `{"stream": "ORDERS"}`, At: time.Now()})

	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, map[string]any{"consumer": "billing", "pending": 12.0, "ack_pending": 3.0, "redelivered": 1.0, "waiting": 2.0}, res.Rows[1])
	keys := []string{}
	for _, c := range res.Columns {
		keys = append(keys, c.Key)
	}
	assert.Equal(t, []string{"consumer", "pending", "ack_pending", "redelivered", "waiting"}, keys)

	past, err := newAdapter().Table(context.Background(), source(srv.URL), nil, domain.TableQuery{Query: `{"stream": "ORDERS"}`, At: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	assert.Empty(t, past.Rows)
}

func TestDiscoverListsStreamsAndConsumers(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/jsz": reply(200, jsz)})

	inv, err := newAdapter().Discover(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"consumer/EMAILS/sender", "consumer/ORDERS/audit", "consumer/ORDERS/billing", "stream/EMAILS", "stream/ORDERS",
	}, inv.Metrics)

	a := newAdapter()
	a.SetMaxInventory(2)
	inv, err = a.Discover(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.True(t, inv.Truncated)
	assert.Len(t, inv.Metrics, 2)
}

func TestTheTokenNeverAppearsInAnError(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/varz": func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"error": "bad token %s"}`, r.Header.Get("Authorization"))
	}})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL), map[string]string{"token": "s3cret-token"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 403)")
	assert.NotContains(t, err.Error(), "s3cret-token")
}

func TestTheURLPolicyJudgesTheMonitoringURL(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/varz": reply(200, `{"server_id": "NA1", "version": "2.11.4"}`)})

	for _, raw := range []string{"http://169.254.169.254:8222", "http://[fd20:ce::254]", "http://metadata.goog"} {
		_, err := newAdapter().Verify(context.Background(), source(raw), nil)
		assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%s: %v", raw, err)
	}
	shared := nats.New()
	shared.SetURLPolicy(urlguard.PublicOnly())
	_, err := shared.Verify(context.Background(), source(srv.URL), nil)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	_, err = nats.New().Verify(context.Background(), source(srv.URL), nil)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, rec.requests)
}

func TestAQueryThatIsNotNATSJSONIsTheCallersMistake(t *testing.T) {
	for _, q := range []string{`nats_consumer_num_pending`, `{"consumer": "c"}`, `{"stream": "S", "metric": "lag"}`} {
		_, err := newAdapter().Series(context.Background(), source("http://127.0.0.1:1"), nil, instant(q))
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
		_, err = newAdapter().Table(context.Background(), source("http://127.0.0.1:1"), nil, domain.TableQuery{Query: q})
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
	}
}

func TestSchemaMarksTheTokenSecret(t *testing.T) {
	secret := map[string]bool{}
	for _, f := range newAdapter().Schema().Fields {
		secret[f.Key] = f.IsSecret()
	}
	assert.Equal(t, map[string]bool{"url": false, "token": true}, secret)
}

func TestDiscoverQueuesOffersEachConsumerByItsStreamAndName(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/jsz": reply(200, jsz)})

	targets, err := newAdapter().DiscoverQueues(context.Background(), source(srv.URL), nil)

	require.NoError(t, err)
	names := []string{}
	for _, tg := range targets {
		names = append(names, tg.Name)
	}
	assert.Equal(t, []string{"audit", "billing", "sender"}, names)
	assert.Equal(t, map[string]string{"stream": "ORDERS", "consumer": "billing"}, targets[1].Params)
}
