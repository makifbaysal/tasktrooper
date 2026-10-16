package domain

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

// Signal is a vendor-neutral measure ("http.requests", "queue.backlog"). The
// catalog is fixed; a recipe maps signals onto one source kind's native
// queries.
type Signal struct {
	ID          string            `json:"id"`
	Kind        SignalKind        `json:"kind"`
	Unit        string            `json:"unit"`
	Roles       []ComponentRole   `json:"roles"`
	Aggregation SignalAggregation `json:"aggregation"`
}

type RecipeOrigin string

const (
	RecipeOriginCatalog RecipeOrigin = "catalog"
	RecipeOriginUser    RecipeOrigin = "user"
)

type RecipeAppliesTo struct {
	Technology      string `json:"technology"`
	MessagingSystem string `json:"messaging_system"`
}

// RecipeDetect is how discovery suggests a recipe: Metric present in a
// source's inventory.
type RecipeDetect struct {
	Metric string `json:"metric"`
}

// Recipe maps signal ids to native queries for one source kind. A query may
// reference a declared param as $name, and the built-ins $__rate (a rate
// window derived from the step) and $__interval (the step).
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
