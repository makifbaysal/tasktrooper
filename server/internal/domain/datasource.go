package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DataSourceKind names a connection type in the data source registry
// ("prometheus"). It is a registry key, not a vendor: one kind covers every
// backend that speaks its API.
type DataSourceKind string

const (
	DataSourceKindPrometheus    DataSourceKind = "prometheus"
	DataSourceKindGCPMonitoring DataSourceKind = "gcp_monitoring"
	DataSourceKindCloudWatch    DataSourceKind = "aws_cloudwatch"
	DataSourceKindKafka         DataSourceKind = "kafka"
	DataSourceKindRabbitMQ      DataSourceKind = "rabbitmq"
	DataSourceKindNATS          DataSourceKind = "nats"
)

// BuiltinDataSourceKinds are the kinds this build ships an adapter for. A
// catalog recipe may name one even where it is not registered, so a
// deployment that leaves a kind out still loads the shared catalog.
func BuiltinDataSourceKinds() []DataSourceKind {
	return []DataSourceKind{
		DataSourceKindPrometheus, DataSourceKindGCPMonitoring, DataSourceKindCloudWatch,
		DataSourceKindKafka, DataSourceKindRabbitMQ, DataSourceKindNATS,
		DataSourceKindLoki, DataSourceKindElasticsearch, DataSourceKindTempo,
		DataSourceKindGooglePlayVitals, DataSourceKindAppStoreMetrics, DataSourceKindSentry,
		DataSourceKindPostHog, DataSourceKindGA4, DataSourceKindPlausible,
		DataSourceKindDatadog, DataSourceKindNewRelic,
	}
}

type Capability string

const (
	CapabilityMetrics Capability = "metrics"
	CapabilityLogs    Capability = "logs"
	CapabilityTraces  Capability = "traces"
	CapabilityErrors  Capability = "errors"
	CapabilityCrashes Capability = "crashes"
	CapabilityRUM     Capability = "rum"
	CapabilityQueues  Capability = "queues"
)

func AllCapabilities() []Capability {
	return []Capability{
		CapabilityMetrics, CapabilityLogs, CapabilityTraces, CapabilityErrors,
		CapabilityCrashes, CapabilityRUM, CapabilityQueues,
	}
}

func ValidCapability(c Capability) bool {
	return slices.Contains(AllCapabilities(), c)
}

type DataSourceStatus string

const (
	DataSourceOK         DataSourceStatus = "ok"
	DataSourceError      DataSourceStatus = "error"
	DataSourceUnverified DataSourceStatus = "unverified"
)

type DataSourceInfo struct {
	Version string `json:"version"`
	Detail  string `json:"detail"`
}

// DataSource is one configured connection. Config holds only the non-secret
// fields of its kind's schema; secret values live encrypted in the store and
// only their names surface, as SecretsSet.
type DataSource struct {
	ID           uuid.UUID         `json:"id"`
	Kind         DataSourceKind    `json:"kind"`
	Name         string            `json:"name"`
	Config       map[string]string `json:"config"`
	SecretsSet   []string          `json:"secrets_set"`
	Status       DataSourceStatus  `json:"status"`
	StatusDetail string            `json:"status_detail"`
	Info         DataSourceInfo    `json:"info"`
	VerifiedAt   *time.Time        `json:"verified_at"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	// Capabilities is the kind's, filled on the way out; never stored.
	Capabilities []Capability `json:"capabilities"`
}

// DataSourceVerification is one verify outcome. A nil VerifiedAt keeps the
// stored one: it marks the last success, and a failure is not one.
type DataSourceVerification struct {
	Status       DataSourceStatus
	StatusDetail string
	Info         DataSourceInfo
	VerifiedAt   *time.Time
}

type DataSourceFieldType string

const (
	FieldText    DataSourceFieldType = "text"
	FieldURL     DataSourceFieldType = "url"
	FieldSecret  DataSourceFieldType = "secret"
	FieldSelect  DataSourceFieldType = "select"
	FieldHeaders DataSourceFieldType = "headers"
	// FieldCloudAccount holds the id of a connected cloud account of the
	// field's Provider. The kind reads that account's credential when it
	// calls, so the source stores no copy of it.
	FieldCloudAccount DataSourceFieldType = "cloud_account"
	// FieldStoreCredential names the workspace's store credential of the
	// field's Provider (StoreCredentialGooglePlay or StoreCredentialASC; there
	// is one per provider), and its value is that provider. The kind reads the
	// credential when it calls, so the source stores no copy of it.
	FieldStoreCredential DataSourceFieldType = "store_credential"
)

// DataSourceField is one entry of a kind's config form. ShowWhen hides the
// field (and drops any stored value of it) unless every named field holds
// the given value; Required applies only while the field is shown.
type DataSourceField struct {
	Key         string              `json:"key"`
	Label       string              `json:"label"`
	Help        string              `json:"help"`
	Type        DataSourceFieldType `json:"type"`
	Required    bool                `json:"required"`
	Options     []string            `json:"options"`
	Placeholder string              `json:"placeholder"`
	ShowWhen    map[string]string   `json:"show_when"`
	Secret      bool                `json:"secret"`
	// Provider is the cloud provider of a cloud_account field, or the store
	// credential provider of a store_credential field.
	Provider CloudProviderKind `json:"provider,omitempty"`
	// Default is the value a shown field takes when none is given, so what is
	// stored (and what an origin move is judged against) is never empty.
	Default string `json:"default,omitempty"`
	// Destination marks a text field holding the host[:port] list a kind
	// connects to (Kafka's bootstrap servers, see ParseHostList): the URL
	// policy judges it when it is saved, and moving it to other hosts
	// re-asks for the stored secrets as moving a url field's origin does.
	Destination bool `json:"destination,omitempty"`
	// Multiline marks a field whose value spans lines (a PEM certificate, a
	// key file's JSON), so a form gives it a text area.
	Multiline bool `json:"multiline,omitempty"`
}

// IsSecret: a headers field is always secret, since it is where an
// Authorization or tenant header goes.
func (f DataSourceField) IsSecret() bool {
	return f.Secret || f.Type == FieldSecret || f.Type == FieldHeaders
}

// Shown reports whether values (the merged config, secrets included) satisfy
// the field's ShowWhen.
func (f DataSourceField) Shown(values map[string]string) bool {
	for k, want := range f.ShowWhen {
		if values[k] != want {
			return false
		}
	}
	return true
}

type DataSourceSchema struct {
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Fields      []DataSourceField `json:"fields"`
}

func (s DataSourceSchema) Field(key string) (DataSourceField, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return DataSourceField{}, false
}

// DataSourceKindInfo is one row of the kind registry as the API lists it.
type DataSourceKindInfo struct {
	Kind         DataSourceKind    `json:"kind"`
	Label        string            `json:"label"`
	Description  string            `json:"description"`
	Capabilities []Capability      `json:"capabilities"`
	Fields       []DataSourceField `json:"fields"`
}

// SaveDataSourceRequest is POST /v1/data-sources. On a create an empty secret
// value means "not set".
type SaveDataSourceRequest struct {
	Kind    DataSourceKind    `json:"kind"`
	Name    string            `json:"name"`
	Config  map[string]string `json:"config"`
	Secrets map[string]string `json:"secrets"`
}

// PatchDataSourceRequest merges: a key given in Config or Secrets replaces
// the stored value and an empty string removes it; keys not given are kept.
type PatchDataSourceRequest struct {
	Name    *string           `json:"name"`
	Config  map[string]string `json:"config"`
	Secrets map[string]string `json:"secrets"`
}

// Inventory is what a source says it holds; Truncated when the list stops
// short of everything it has.
type Inventory struct {
	Metrics   []string `json:"metrics"`
	Truncated bool     `json:"truncated"`
}

// DataSourceDiscovery is a source's inventory as the API answers it, with
// the recipes whose detect metric it holds.
type DataSourceDiscovery struct {
	Metrics   []string      `json:"metrics"`
	Truncated bool          `json:"truncated"`
	Recipes   []RecipeMatch `json:"recipes"`
}

type SeriesQuery struct {
	Query string
	From  time.Time
	To    time.Time
	Step  time.Duration
}

// SeriesPoint is one sample; Value is nil where the source answered NaN or
// ±Inf, which JSON cannot carry.
type SeriesPoint struct {
	Time  time.Time
	Value *float64
}

// MarshalJSON writes the [unix_ms, value_or_null] pair charts consume.
func (p SeriesPoint) MarshalJSON() ([]byte, error) {
	if p.Value == nil {
		return json.Marshal([2]any{p.Time.UnixMilli(), nil})
	}
	return json.Marshal([2]any{p.Time.UnixMilli(), *p.Value})
}

// FinitePoint keeps NaN and ±Inf out of a SeriesPoint's Value.
func FinitePoint(t time.Time, v float64) SeriesPoint {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return SeriesPoint{Time: t}
	}
	return SeriesPoint{Time: t, Value: &v}
}

type Series struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Points []SeriesPoint     `json:"points"`
}

// SeriesResult: Step is the spacing the source answered at where the kind
// decides it (CloudWatch's period); zero means the step asked for.
type SeriesResult struct {
	Series   []Series      `json:"series"`
	Warnings []string      `json:"warnings"`
	Step     time.Duration `json:"-"`
}

type HeaderLine struct {
	Name  string
	Value string
}

// forbiddenHeaders: the framing ones are set by the HTTP client itself, and a
// stored value overriding one would desynchronise the request from its
// framing; the metadata ones are what a cloud metadata service requires
// before it answers, which no data source has a reason to send.
var forbiddenHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true,
	"connection": true, "upgrade": true, "te": true, "trailer": true,
	"metadata-flavor": true, "x-aws-ec2-metadata-token": true, "x-aws-ec2-metadata-token-ttl-seconds": true,
}

// ParseHeaderLines reads a headers field: one "Name: value" per line, blank
// lines ignored.
func ParseHeaderLines(raw string) ([]HeaderLine, error) {
	var out []HeaderLine
	for i, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || !validHeaderName(name) {
			return nil, fmt.Errorf("line %d is not \"Name: value\"", i+1)
		}
		if forbiddenHeaders[strings.ToLower(name)] {
			return nil, fmt.Errorf("line %d sets %s, which cannot be set", i+1, name)
		}
		value = strings.TrimSpace(value)
		if strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("line %d has a control character in its value", i+1)
		}
		out = append(out, HeaderLine{Name: name, Value: value})
	}
	return out, nil
}

func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}
