package domain

import "strings"

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
// (see ResourceRefValue for the paths).
type RecipeAppliesTo struct {
	Technology      string            `json:"technology"`
	MessagingSystem string            `json:"messaging_system"`
	ResourceKind    CloudResourceKind `json:"resource_kind"`
	ResourceParams  map[string]string `json:"resource_params"`
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
// query language's rule.
type Recipe struct {
	ID         string            `json:"id"`
	SourceKind DataSourceKind    `json:"source_kind"`
	AppliesTo  RecipeAppliesTo   `json:"applies_to"`
	Detect     RecipeDetect      `json:"detect"`
	Params     []string          `json:"params"`
	Signals    map[string]string `json:"signals"`
	Origin     RecipeOrigin      `json:"origin"`
}

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
