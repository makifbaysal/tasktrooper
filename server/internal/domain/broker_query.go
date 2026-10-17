package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// The direct broker kinds (kafka, rabbitmq, nats) read a broker's live state,
// not a metrics store: a query answers one point per series, at the instant
// it is read. Each kind's query is one small JSON object naming the entity
// and the measure; an empty metric is the kind's backlog measure.

const (
	KafkaMetricLag            = "lag"
	KafkaMetricLagByPartition = "lag_by_partition"
	KafkaMetricConsumers      = "consumers"
	KafkaMetricMembers        = "members"

	RabbitMQMetricMessages    = "messages"
	RabbitMQMetricReady       = "ready"
	RabbitMQMetricUnacked     = "unacked"
	RabbitMQMetricConsumers   = "consumers"
	RabbitMQMetricPublishRate = "publish_rate"
	RabbitMQMetricDeliverRate = "deliver_rate"
	RabbitMQMetricAckRate     = "ack_rate"

	NATSMetricPending     = "pending"
	NATSMetricAckPending  = "ack_pending"
	NATSMetricRedelivered = "redelivered"
	NATSMetricWaiting     = "waiting"
)

var (
	kafkaMetrics    = []string{KafkaMetricLag, KafkaMetricLagByPartition, KafkaMetricConsumers, KafkaMetricMembers}
	rabbitMQMetrics = []string{
		RabbitMQMetricMessages, RabbitMQMetricReady, RabbitMQMetricUnacked, RabbitMQMetricConsumers,
		RabbitMQMetricPublishRate, RabbitMQMetricDeliverRate, RabbitMQMetricAckRate,
	}
	natsMetrics = []string{NATSMetricPending, NATSMetricAckPending, NATSMetricRedelivered, NATSMetricWaiting}
)

const maxBrokerName = 1024

// KafkaQuery is the query language of the kafka kind:
//
//	{"group": "billing", "topic": "invoices", "metric": "lag"}
//
// Topic, when set, keeps only that topic's partitions. Metric is lag (the
// default: the group's total), lag_by_partition (one series per partition),
// consumers (members assigned a partition) or members (every member).
type KafkaQuery struct {
	Group  string `json:"group"`
	Topic  string `json:"topic,omitempty"`
	Metric string `json:"metric,omitempty"`
}

// RabbitMQQuery is the query language of the rabbitmq kind:
//
//	{"queue": "orders.*", "metric": "ready"}
//
// A * in Queue matches any run of characters, so one query can read every
// queue of a family (one series or row each). Metric is messages (the
// default: ready plus unacknowledged), ready, unacked, consumers, or the
// per-second publish_rate, deliver_rate (deliveries and gets) and ack_rate.
type RabbitMQQuery struct {
	Queue  string `json:"queue"`
	Metric string `json:"metric,omitempty"`
}

// NATSQuery is the query language of the nats kind, over JetStream:
//
//	{"stream": "ORDERS", "consumer": "billing", "metric": "pending"}
//
// Consumer, when empty, reads every consumer of the stream. Metric is
// pending (the default: messages not yet delivered), ack_pending,
// redelivered or waiting (pull requests waiting).
type NATSQuery struct {
	Stream   string `json:"stream"`
	Consumer string `json:"consumer,omitempty"`
	Metric   string `json:"metric,omitempty"`
}

func ParseKafkaQuery(raw string) (KafkaQuery, error) {
	var q KafkaQuery
	if err := decodeBrokerQuery(raw, "Kafka", &q); err != nil {
		return KafkaQuery{}, err
	}
	if err := checkBrokerName("group", q.Group, true); err != nil {
		return KafkaQuery{}, err
	}
	if err := checkBrokerName("topic", q.Topic, false); err != nil {
		return KafkaQuery{}, err
	}
	m, err := brokerMetric(q.Metric, KafkaMetricLag, kafkaMetrics)
	q.Metric = m
	return q, err
}

func ParseRabbitMQQuery(raw string) (RabbitMQQuery, error) {
	var q RabbitMQQuery
	if err := decodeBrokerQuery(raw, "RabbitMQ", &q); err != nil {
		return RabbitMQQuery{}, err
	}
	if err := checkBrokerName("queue", q.Queue, true); err != nil {
		return RabbitMQQuery{}, err
	}
	m, err := brokerMetric(q.Metric, RabbitMQMetricMessages, rabbitMQMetrics)
	q.Metric = m
	return q, err
}

func ParseNATSQuery(raw string) (NATSQuery, error) {
	var q NATSQuery
	if err := decodeBrokerQuery(raw, "NATS", &q); err != nil {
		return NATSQuery{}, err
	}
	if err := checkBrokerName("stream", q.Stream, true); err != nil {
		return NATSQuery{}, err
	}
	if err := checkBrokerName("consumer", q.Consumer, false); err != nil {
		return NATSQuery{}, err
	}
	m, err := brokerMetric(q.Metric, NATSMetricPending, natsMetrics)
	q.Metric = m
	return q, err
}

func decodeBrokerQuery(raw, kind string, out any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("not a %s query: %w", kind, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("not a %s query: more than one JSON value", kind)
	}
	return nil
}

func checkBrokerName(key, v string, required bool) error {
	switch {
	case strings.TrimSpace(v) == "":
		if required {
			return fmt.Errorf("%s is required", key)
		}
		if v != "" {
			return fmt.Errorf("%s is blank", key)
		}
	case len(v) > maxBrokerName:
		return fmt.Errorf("%s is longer than %d bytes", key, maxBrokerName)
	case strings.ContainsFunc(v, unicode.IsControl):
		return fmt.Errorf("%s has a control character", key)
	}
	return nil
}

func brokerMetric(m, def string, known []string) (string, error) {
	if m == "" {
		return def, nil
	}
	if !slices.Contains(known, m) {
		return "", fmt.Errorf("metric %q is not one of %s", m, strings.Join(known, ", "))
	}
	return m, nil
}

const maxHosts = 64

// ParseHostList reads a comma-separated host[:port] list (a Kafka bootstrap
// list) into lowercase host:port entries, defaultPort where an entry names
// none; with no defaultPort such an entry stays a bare host. An IPv6 address
// with a port is bracketed ([::1]:9092).
func ParseHostList(raw, defaultPort string) ([]string, error) {
	var out []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		hp, err := hostPort(entry, defaultPort)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", entry, err)
		}
		if !slices.Contains(out, hp) {
			out = append(out, hp)
		}
	}
	switch {
	case len(out) == 0:
		return nil, errors.New("no host is given")
	case len(out) > maxHosts:
		return nil, fmt.Errorf("more than %d hosts", maxHosts)
	}
	return out, nil
}

func hostPort(entry, defaultPort string) (string, error) {
	if strings.Contains(entry, "://") || strings.ContainsAny(entry, "/@?# \t") {
		return "", errors.New("give host:port, not a URL")
	}
	if inner, ok := strings.CutPrefix(entry, "["); ok && strings.HasSuffix(inner, "]") && net.ParseIP(inner[:len(inner)-1]) != nil {
		entry = inner[:len(inner)-1]
	}
	host, port, explicit := entry, defaultPort, false
	if net.ParseIP(entry) == nil {
		if h, p, err := net.SplitHostPort(entry); err == nil {
			if strings.HasPrefix(entry, "[") && net.ParseIP(h) == nil {
				return "", errors.New("only an IPv6 address goes in brackets")
			}
			host, port, explicit = h, p, true
		} else if strings.ContainsAny(entry, "[]:") {
			return "", errors.New("not host:port")
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || strings.ContainsAny(host, "[]") {
		return "", errors.New("no host")
	}
	if port == "" && !explicit {
		return host, nil
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", errors.New("the port is not 1-65535")
	}
	return net.JoinHostPort(host, port), nil
}

// BrokerTarget is what a direct broker kind's query reads, its measure left
// out, as canonical JSON: each kind's table answers the entity whatever the
// metric, so two queries differing only in it read the same table. ok is
// false for another kind or a query that does not parse.
func BrokerTarget(kind DataSourceKind, query string) (string, bool) {
	var (
		v   any
		err error
	)
	switch kind {
	case DataSourceKindKafka:
		var q KafkaQuery
		q, err = ParseKafkaQuery(query)
		q.Metric = ""
		v = q
	case DataSourceKindRabbitMQ:
		var q RabbitMQQuery
		q, err = ParseRabbitMQQuery(query)
		q.Metric = ""
		v = q
	case DataSourceKindNATS:
		var q NATSQuery
		q, err = ParseNATSQuery(query)
		q.Metric = ""
		v = q
	default:
		return "", false
	}
	if err != nil {
		return "", false
	}
	b, err := json.Marshal(v)
	return string(b), err == nil
}

// LiveOnlyKind: a direct broker kind reads its broker's state as it is now,
// so whatever window it is asked for it answers one point, at the instant
// read, and no window that ended earlier.
func LiveOnlyKind(k DataSourceKind) bool {
	switch k {
	case DataSourceKindKafka, DataSourceKindRabbitMQ, DataSourceKindNATS:
		return true
	}
	return false
}
