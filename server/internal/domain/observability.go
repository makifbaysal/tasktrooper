package domain

import (
	"slices"
	"strings"
)

// SignalKind is the shape of what a signal measures.
type SignalKind string

const (
	SignalMetric SignalKind = "metric"
	SignalLog    SignalKind = "log"
	SignalTrace  SignalKind = "trace"
	SignalError  SignalKind = "error"
)

func ValidSignalKind(k SignalKind) bool {
	switch k {
	case SignalMetric, SignalLog, SignalTrace, SignalError:
		return true
	}
	return false
}

// SignalAggregation is how one value of a signal is formed over a step.
type SignalAggregation string

const (
	AggregationRate     SignalAggregation = "rate"
	AggregationRatio    SignalAggregation = "ratio"
	AggregationQuantile SignalAggregation = "quantile"
	AggregationGauge    SignalAggregation = "gauge"
)

func ValidSignalAggregation(a SignalAggregation) bool {
	switch a {
	case AggregationRate, AggregationRatio, AggregationQuantile, AggregationGauge:
		return true
	}
	return false
}

// SignalDirection is which way a signal moves when things get better. A
// signal with none (an instance count, a rollout fraction) is not judged.
type SignalDirection string

const (
	SignalHigherIsBetter SignalDirection = "higher"
	SignalLowerIsBetter  SignalDirection = "lower"
)

func ValidSignalDirection(d SignalDirection) bool {
	return d == "" || d == SignalHigherIsBetter || d == SignalLowerIsBetter
}

// Signal is a vendor-neutral measure ("http.requests", "queue.backlog"). The
// catalog is fixed; a recipe maps signals onto one source kind's native
// queries.
type Signal struct {
	ID          string            `json:"id"`
	Kind        SignalKind        `json:"kind"`
	Unit        string            `json:"unit"`
	Roles       []ComponentRole   `json:"roles"`
	Aggregation SignalAggregation `json:"aggregation"`
	Better      SignalDirection   `json:"better,omitempty"`
}

// The catalog signals a release's soak thresholds read.
const (
	SignalHTTPErrorRatio = "http.error_ratio"
	SignalHTTPLatency    = "http.latency"
)

type RecipeOrigin string

const (
	RecipeOriginCatalog RecipeOrigin = "catalog"
	RecipeOriginUser    RecipeOrigin = "user"
)

// RecipeAppliesTo says what a recipe reads. ResourceKind is the cloud
// resource an environment must be bound to for the recipe to fit it, and
// ResourceParams says which field of that resource's ref fills which param
// (see ResourceRefValue for the paths). StorePlatform is the store app
// (MobileStorePlatformIOS or MobileStorePlatformAndroid) a component's
// repository must have linked for the recipe to fit it, and StoreParams
// says which field of that app fills which param (see StoreAppValue).
type RecipeAppliesTo struct {
	Technology      string            `json:"technology"`
	MessagingSystem string            `json:"messaging_system"`
	ResourceKind    CloudResourceKind `json:"resource_kind"`
	ResourceParams  map[string]string `json:"resource_params"`
	StorePlatform   string            `json:"store_platform"`
	StoreParams     map[string]string `json:"store_params"`
}

// RecipeDetect is how discovery suggests a recipe: Metric present in a
// source's inventory.
type RecipeDetect struct {
	Metric string `json:"metric"`
}

// Recipe maps signal ids to native queries for one source kind. A query may
// reference a declared param as $name, and the built-ins $__rate (a rate
// window derived from the step), $__interval (the step) and $__quantile (the
// quantile a quantile signal is read at); where each may sit is the kind's
// query language's rule. Tables are the per-entity tables it reads at an
// instant, by name (RecipeTableQueues). Defaults is the value a declared
// param takes where a binding leaves it empty (WithDefaults).
type Recipe struct {
	ID         string                 `json:"id"`
	SourceKind DataSourceKind         `json:"source_kind"`
	AppliesTo  RecipeAppliesTo        `json:"applies_to"`
	Detect     RecipeDetect           `json:"detect"`
	Params     []string               `json:"params"`
	Defaults   map[string]string      `json:"defaults,omitempty"`
	Signals    map[string]string      `json:"signals"`
	Tables     map[string]RecipeTable `json:"tables,omitempty"`
	Origin     RecipeOrigin           `json:"origin"`
}

// RecipeTableQueues is the table the queues view reads: one row per queue,
// partition or subscription the recipe's params select.
const RecipeTableQueues = "queues"

// RecipeTable is one table a recipe reads at an instant. Columns are shown in
// order. A column named in KeyLabels is filled from that label of each result
// and identifies the row; every other column is the value its own query in
// Queries answers, so one row joins a result of each query on the key
// labels' values.
type RecipeTable struct {
	Columns   []TableColumn     `json:"columns"`
	KeyLabels []string          `json:"key_labels"`
	Queries   map[string]string `json:"queries"`
}

// ValueColumns are the columns a query fills, in column order.
func (t RecipeTable) ValueColumns() []TableColumn {
	var out []TableColumn
	for _, c := range t.Columns {
		if !t.IsKey(c.Key) {
			out = append(out, c)
		}
	}
	return out
}

func (t RecipeTable) IsKey(column string) bool { return slices.Contains(t.KeyLabels, column) }

// RecipeMatch is one recipe discovery found evidence for.
type RecipeMatch struct {
	RecipeID      string `json:"recipe_id"`
	MatchedMetric string `json:"matched_metric"`
}

const resourceRefExtraPrefix = "extra."

// ResourceRefValue reads the field of ref a recipe's resource_params names:
// "id", "name", "region", or "extra.<key>".
func ResourceRefValue(ref CloudResourceRef, path string) (string, bool) {
	switch path {
	case "id":
		return ref.ID, true
	case "name":
		return ref.Name, true
	case "region":
		return ref.Region, true
	}
	if key, ok := strings.CutPrefix(path, resourceRefExtraPrefix); ok && key != "" {
		return ref.Extra[key], true
	}
	return "", false
}

func ValidResourceRefPath(path string) bool {
	_, ok := ResourceRefValue(CloudResourceRef{}, path)
	return ok
}

// ParamsFromResource fills the params ResourceParams maps from ref. A ref
// field that is empty is left out, so the caller sees which params still
// need a value.
func (r Recipe) ParamsFromResource(ref CloudResourceRef) map[string]string {
	out := map[string]string{}
	for param, path := range r.AppliesTo.ResourceParams {
		if v, ok := ResourceRefValue(ref, path); ok && v != "" {
			out[param] = v
		}
	}
	return out
}
