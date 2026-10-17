package kafka_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/kafka"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const topic = "orders"

func cluster(t *testing.T, opts ...kfake.Opt) *kfake.Cluster {
	t.Helper()
	base := []kfake.Opt{kfake.NumBrokers(1), kfake.SeedTopics(3, topic), kfake.ClusterID("c-test")}
	c, err := kfake.NewCluster(append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func newAdapter() *kafka.Adapter {
	a := kafka.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(c *kfake.Cluster, config map[string]string) domain.DataSource {
	cfg := map[string]string{"bootstrap_servers": strings.Join(c.ListenAddrs(), ",")}
	maps.Copy(cfg, config)
	return domain.DataSource{Kind: domain.DataSourceKindKafka, Config: cfg}
}

func client(t *testing.T, c *kfake.Cluster, opts ...kgo.Opt) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(append([]kgo.Opt{kgo.SeedBrokers(c.ListenAddrs()...)}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(cl.Close)
	return cl
}

// produce writes n records to each partition given.
func produce(t *testing.T, c *kfake.Cluster, perPartition map[int32]int) {
	t.Helper()
	cl := client(t, c, kgo.RecordPartitioner(kgo.ManualPartitioner()), kgo.DefaultProduceTopic(topic))
	for p, n := range perPartition {
		for i := 0; i < n; i++ {
			require.NoError(t, cl.ProduceSync(context.Background(), &kgo.Record{Partition: p, Value: []byte("x")}).FirstErr())
		}
	}
}

func commit(t *testing.T, c *kfake.Cluster, group string, offsets map[int32]int64) {
	t.Helper()
	var os kadm.Offsets
	for p, at := range offsets {
		os.AddOffset(topic, p, at, -1)
	}
	require.NoError(t, kadm.NewClient(client(t, c)).CommitAllOffsets(context.Background(), group, os))
}

// join starts a member of group consuming the topic and waits for its
// assignment.
func join(t *testing.T, c *kfake.Cluster, group string, opts ...kgo.Opt) {
	t.Helper()
	assigned := make(chan struct{})
	var once sync.Once
	opts = append([]kgo.Opt{
		kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic), kgo.ClientID("billing-worker"),
		kgo.DisableAutoCommit(), kgo.FetchMaxWait(100 * time.Millisecond),
		kgo.OnPartitionsAssigned(func(context.Context, *kgo.Client, map[string][]int32) { once.Do(func() { close(assigned) }) }),
	}, opts...)
	cl := client(t, c, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			cl.PollFetches(ctx)
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-assigned:
	case <-time.After(15 * time.Second):
		t.Fatal("the member was never assigned a partition")
	}
}

func instant(query string) domain.SeriesQuery {
	now := time.Now()
	return domain.SeriesQuery{Query: query, From: now.Add(-time.Hour), To: now, Step: time.Minute}
}

func value(t *testing.T, s domain.Series) float64 {
	t.Helper()
	require.Len(t, s.Points, 1)
	require.NotNil(t, s.Points[0].Value)
	return *s.Points[0].Value
}

func TestVerifyReadsTheClusterMetadata(t *testing.T) {
	c := cluster(t)

	info, err := newAdapter().Verify(context.Background(), source(c, nil), nil)

	require.NoError(t, err)
	assert.Equal(t, "Kafka · 1 broker · cluster c-test", info.Detail)
	assert.NotEmpty(t, info.Version)
}

func TestSeriesReadsTheGroupsLagAsOneLivePoint(t *testing.T) {
	c := cluster(t)
	produce(t, c, map[int32]int{0: 5, 1: 3, 2: 2})
	commit(t, c, "billing", map[int32]int64{0: 2, 1: 3})
	a := newAdapter()
	ds := source(c, nil)

	q := instant(`{"group": "billing"}`)
	res, err := a.Series(context.Background(), ds, nil, q)
	require.NoError(t, err)
	require.Len(t, res.Series, 1)
	assert.Equal(t, "lag", res.Series[0].Name)
	assert.Equal(t, map[string]string{"group": "billing"}, res.Series[0].Labels)
	assert.Equal(t, 5.0, value(t, res.Series[0]), "partition 0 is 3 behind, 1 caught up, 2 never committed to and 2 long")
	assert.WithinDuration(t, q.To, res.Series[0].Points[0].Time, time.Second)

	res, err = a.Series(context.Background(), ds, nil, instant(`{"group": "billing", "topic": "orders", "metric": "lag_by_partition"}`))
	require.NoError(t, err)
	byPartition := map[string]float64{}
	for _, s := range res.Series {
		assert.Equal(t, "orders", s.Labels["topic"])
		byPartition[s.Labels["partition"]] = value(t, s)
	}
	assert.Equal(t, map[string]float64{"0": 3, "1": 0, "2": 2}, byPartition)

	res, err = a.Series(context.Background(), ds, nil, instant(`{"group": "billing", "metric": "members"}`))
	require.NoError(t, err)
	assert.Equal(t, 0.0, value(t, res.Series[0]))
}

func TestTableAnswersOneRowPerPartitionWithItsMember(t *testing.T) {
	c := cluster(t)
	produce(t, c, map[int32]int{0: 4, 1: 1, 2: 1})
	commit(t, c, "workers", map[int32]int64{0: 1, 1: 1, 2: 1})
	join(t, c, "workers")

	res, err := newAdapter().Table(context.Background(), source(c, nil), nil, domain.TableQuery{Query: `{"group": "workers"}`, At: time.Now()})

	require.NoError(t, err)
	assert.False(t, res.Truncated)
	require.Len(t, res.Rows, 3)
	first := res.Rows[0]
	assert.Equal(t, "orders", first["topic"])
	assert.Equal(t, 0.0, first["partition"])
	assert.Equal(t, 1.0, first["committed_offset"])
	assert.Equal(t, 4.0, first["end_offset"])
	assert.Equal(t, 3.0, first["lag"])
	assert.Equal(t, "billing-worker", first["client_id"])
	assert.NotEmpty(t, first["member_id"])
	keys := make([]string, 0, len(res.Columns))
	for _, col := range res.Columns {
		keys = append(keys, col.Key)
	}
	assert.Equal(t, []string{"topic", "partition", "committed_offset", "end_offset", "lag", "member_id", "client_id"}, keys)
	_, err = json.Marshal(res.Rows)
	assert.NoError(t, err)

	series, err := newAdapter().Series(context.Background(), source(c, nil), nil, instant(`{"group": "workers", "metric": "consumers"}`))
	require.NoError(t, err)
	assert.Equal(t, 1.0, value(t, series.Series[0]))
}

func TestANextGenerationGroupIsReadThroughItsOwnDescribe(t *testing.T) {
	c := cluster(t)
	produce(t, c, map[int32]int{0: 6, 1: 0, 2: 0})
	commit(t, c, "kip848", map[int32]int64{0: 2})
	join(t, c, "kip848", kgo.ServerSideBalancer(), kgo.Balancers(kgo.RangeBalancer()))
	a := newAdapter()

	res, err := a.Series(context.Background(), source(c, nil), nil, instant(`{"group": "kip848"}`))
	require.NoError(t, err)
	require.Len(t, res.Series, 1, "%v", res.Warnings)
	assert.Equal(t, 4.0, value(t, res.Series[0]))

	res, err = a.Series(context.Background(), source(c, nil), nil, instant(`{"group": "kip848", "metric": "consumers"}`))
	require.NoError(t, err)
	assert.Equal(t, 1.0, value(t, res.Series[0]))
}

func TestAMissingGroupAnswersNoSeriesAndSaysWhy(t *testing.T) {
	c := cluster(t)

	res, err := newAdapter().Series(context.Background(), source(c, nil), nil, instant(`{"group": "nobody"}`))

	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Equal(t, []string{`the cluster has no consumer group "nobody"`}, res.Warnings)
}

func TestAWindowThatEndedEarlierIsNotAnsweredWithTheLiveValue(t *testing.T) {
	a := kafka.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	q := instant(`{"group": "billing"}`)
	q.From, q.To = q.From.Add(-time.Hour), q.To.Add(-time.Hour)

	res, err := a.Series(context.Background(), domain.DataSource{Config: map[string]string{"bootstrap_servers": "127.0.0.1:1"}}, nil, q)

	require.NoError(t, err)
	assert.Empty(t, res.Series)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "live value")
}

func TestDiscoverListsGroupsAndTopicsButNotInternalOnes(t *testing.T) {
	c := cluster(t)
	produce(t, c, map[int32]int{0: 1})
	commit(t, c, "billing", map[int32]int64{0: 1})

	inv, err := newAdapter().Discover(context.Background(), source(c, nil), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"group/billing", "topic/orders"}, inv.Metrics)
	assert.False(t, inv.Truncated)

	a := newAdapter()
	a.SetMaxInventory(1)
	inv, err = a.Discover(context.Background(), source(c, nil), nil)
	require.NoError(t, err)
	assert.Equal(t, domain.Inventory{Metrics: []string{"group/billing"}, Truncated: true}, inv)
}

func TestSASLAuthenticatesAndAFailureNeverCarriesThePassword(t *testing.T) {
	for _, mech := range []struct{ name, kfake string }{{"plain", "PLAIN"}, {"scram-sha-256", "SCRAM-SHA-256"}, {"scram-sha-512", "SCRAM-SHA-512"}} {
		t.Run(mech.name, func(t *testing.T) {
			c := cluster(t, kfake.EnableSASL(), kfake.Superuser(mech.kfake, "ops", "s3cret-pass"))
			ds := source(c, map[string]string{"sasl_mechanism": mech.name, "username": "ops"})

			_, err := newAdapter().Verify(context.Background(), ds, map[string]string{"password": "s3cret-pass"})
			require.NoError(t, err)

			_, err = newAdapter().Verify(context.Background(), ds, map[string]string{"password": "wrong-pass"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "SASL")
			assert.NotContains(t, err.Error(), "wrong-pass")
		})
	}
}

func TestSASLWithoutCredentialsIsRefusedBeforeAnyConnection(t *testing.T) {
	_, err := newAdapter().Verify(context.Background(), domain.DataSource{Config: map[string]string{
		"bootstrap_servers": "127.0.0.1:1", "sasl_mechanism": "plain",
	}}, nil)

	assert.ErrorContains(t, err, "SASL needs a username and a password")
}

func TestTLSTrustsTheSourcesOwnCAEvenPastedOnOneLine(t *testing.T) {
	caPEM, serverCfg := selfSigned(t)
	c := cluster(t, kfake.TLS(serverCfg))
	oneLine := strings.ReplaceAll(caPEM, "\n", " ")

	info, err := newAdapter().Verify(context.Background(), source(c, map[string]string{"tls": "on"}), map[string]string{"ca_cert": oneLine})
	require.NoError(t, err)
	assert.Contains(t, info.Detail, "1 broker")

	_, err = newAdapter().Verify(context.Background(), source(c, map[string]string{"tls": "on"}), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS with 127.0.0.1")

	_, err = newAdapter().Verify(context.Background(), source(c, map[string]string{"tls": "on"}), map[string]string{"ca_cert": "not a pem"})
	assert.ErrorContains(t, err, "not a PEM certificate")
}

func TestEveryBrokerDialGoesThroughTheURLPolicy(t *testing.T) {
	c := cluster(t)
	port := c.ListenAddrs()[0][strings.LastIndex(c.ListenAddrs()[0], ":")+1:]
	var mu sync.Mutex
	var lookups int
	var dialed []string
	policy := urlguard.HumanConfigured()
	policy.Resolver = resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		mu.Lock()
		defer mu.Unlock()
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	policy.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}
	a := kafka.New()
	a.SetURLPolicy(policy)

	_, err := a.Verify(context.Background(), domain.DataSource{Config: map[string]string{"bootstrap_servers": "kafka.lan:" + port}}, nil)

	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, lookups)
	assert.GreaterOrEqual(t, len(dialed), 2, "the seed and the broker the metadata advertises")
	for _, d := range dialed {
		assert.Equal(t, "127.0.0.1:"+port, d)
	}
}

func TestTheURLPolicyRefusesMetadataAndAPublicOnlyPolicyRefusesLoopback(t *testing.T) {
	for _, bootstrap := range []string{"169.254.169.254:9092", "[fd00:ec2::254]:9092", "metadata.google.internal:9092"} {
		_, err := newAdapter().Verify(context.Background(), domain.DataSource{Config: map[string]string{"bootstrap_servers": bootstrap}}, nil)
		require.Error(t, err, bootstrap)
		assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%s: %v", bootstrap, err)
	}

	c := cluster(t)
	shared := kafka.New()
	shared.SetURLPolicy(urlguard.PublicOnly())
	_, err := shared.Verify(context.Background(), source(c, nil), nil)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)

	_, err = kafka.New().Verify(context.Background(), source(c, nil), nil)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "without a policy nothing is dialled: %v", err)
}

func TestAnUnreachableBrokerSaysSoByTheNameGiven(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	policy := urlguard.HumanConfigured()
	policy.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	a := kafka.New()
	a.SetURLPolicy(policy)

	_, err = a.Verify(context.Background(), domain.DataSource{Config: map[string]string{"bootstrap_servers": fmt.Sprintf("kafka.lan:%d", port)}}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("cannot connect to kafka.lan:%d", port))
	assert.NotContains(t, err.Error(), "127.0.0.1")
}

func TestAQueryThatIsNotKafkaJSONIsTheCallersMistake(t *testing.T) {
	a := kafka.New()
	for _, q := range []string{`sum(kafka_consumergroup_lag)`, `{"topic": "orders"}`, `{"group": "g", "metric": "rate"}`} {
		_, err := a.Series(context.Background(), domain.DataSource{}, nil, instant(q))
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
		_, err = a.Table(context.Background(), domain.DataSource{}, nil, domain.TableQuery{Query: q, At: time.Now()})
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
	}
}

func TestSchemaMarksTheSecretsAndTheDestination(t *testing.T) {
	secret := map[string]bool{}
	for _, f := range kafka.New().Schema().Fields {
		secret[f.Key] = f.IsSecret()
		if f.Key == "bootstrap_servers" {
			assert.True(t, f.Destination)
		}
	}
	assert.Equal(t, map[string]bool{
		"bootstrap_servers": false, "sasl_mechanism": false, "username": false, "tls": false,
		"password": true, "ca_cert": true,
	}, secret)
}

type resolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func selfSigned(t *testing.T) (string, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kfake test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

func TestDiscoverQueuesOffersEachGroupByItsName(t *testing.T) {
	c := cluster(t)
	commit(t, c, `billing"x`, map[int32]int64{0: 0})

	targets, err := newAdapter().DiscoverQueues(context.Background(), source(c, nil), nil)

	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, `billing"x`, targets[0].Name)
	assert.Equal(t, map[string]string{"group": `billing"x`}, targets[0].Params)
}

func TestSchemaTellsAFormWhichFieldIsADestinationAndWhichSpansLines(t *testing.T) {
	raw, err := json.Marshal(newAdapter().Schema())
	require.NoError(t, err)
	var schema struct {
		Fields []map[string]any `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	flags := map[string][2]any{}
	for _, f := range schema.Fields {
		flags[f["key"].(string)] = [2]any{f["destination"], f["multiline"]}
	}
	assert.Equal(t, [2]any{true, nil}, flags["bootstrap_servers"])
	assert.Equal(t, [2]any{nil, true}, flags["ca_cert"])
	assert.Equal(t, [2]any{nil, nil}, flags["username"], "an unset flag stays out of the answer")
}
