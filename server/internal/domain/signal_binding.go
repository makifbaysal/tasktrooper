package domain

import (
	"time"

	"github.com/google/uuid"
)

type SignalBindingStatus string

const (
	SignalBindingConfirmed SignalBindingStatus = "confirmed"
	SignalBindingSuggested SignalBindingStatus = "suggested"
	SignalBindingDismissed SignalBindingStatus = "dismissed"
)

type SignalBindingSource string

const (
	SignalBindingDetected SignalBindingSource = "detected"
	SignalBindingHuman    SignalBindingSource = "human"
	SignalBindingAI       SignalBindingSource = "ai"
)

// SignalBinding says where one signal of one component environment is read:
// a data source plus either a recipe and its params, or a raw Query, which
// wins when set. At most one binding per (component, environment, signal) is
// confirmed; suggestions may be several, and a dismissed one is kept so the
// detector does not offer it again.
type SignalBinding struct {
	ID           uuid.UUID           `json:"id"`
	ComponentID  uuid.UUID           `json:"component_id"`
	Environment  DeployEnvironment   `json:"environment"`
	Signal       string              `json:"signal"`
	DataSourceID uuid.UUID           `json:"data_source_id"`
	RecipeID     string              `json:"recipe_id"`
	Params       map[string]string   `json:"params"`
	Query        string              `json:"query"`
	Status       SignalBindingStatus `json:"status"`
	Source       SignalBindingSource `json:"source"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// SameTarget reports whether two bindings read the same thing, whatever
// their status: the detector's test for "already offered" and "dismissed".
func (b SignalBinding) SameTarget(o SignalBinding) bool {
	if b.DataSourceID != o.DataSourceID || b.RecipeID != o.RecipeID || b.Query != o.Query || len(b.Params) != len(o.Params) {
		return false
	}
	for k, v := range b.Params {
		if ov, ok := o.Params[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

// SignalBindingView is a binding as the API answers it. RenderedQuery is the
// query at a one-minute step, empty while NeedsParams names a recipe param
// without a value.
type SignalBindingView struct {
	SignalBinding
	DataSourceName string         `json:"data_source_name"`
	DataSourceKind DataSourceKind `json:"data_source_kind"`
	RenderedQuery  string         `json:"rendered_query"`
	NeedsParams    []string       `json:"needs_params"`
}

type SignalBindingState string

const (
	SignalStateBound     SignalBindingState = "bound"
	SignalStateSuggested SignalBindingState = "suggested"
	SignalStateUnbound   SignalBindingState = "unbound"
)

type ComponentSignal struct {
	Signal      string              `json:"signal"`
	Kind        SignalKind          `json:"kind"`
	Unit        string              `json:"unit"`
	Golden      bool                `json:"golden"`
	Status      SignalBindingState  `json:"status"`
	Binding     *SignalBindingView  `json:"binding"`
	Suggestions []SignalBindingView `json:"suggestions"`
}

// ComponentSignals is GET /v1/components/:id/signals.
type ComponentSignals struct {
	ComponentID uuid.UUID         `json:"component_id"`
	Role        ComponentRole     `json:"role"`
	Environment DeployEnvironment `json:"environment"`
	Signals     []ComponentSignal `json:"signals"`
	// SourceOffers are filled by the detector only: data sources it would
	// read the component through if one of that kind existed.
	SourceOffers []DataSourceOffer `json:"source_offers"`
}

// SignalPreview is a recipe or query run once before it is bound.
type SignalPreview struct {
	RenderedQuery string   `json:"rendered_query"`
	Series        []Series `json:"series"`
	Warnings      []string `json:"warnings"`
}

type SignalReadErrorCode string

const (
	SignalErrUnknown     SignalReadErrorCode = "unknown_signal"
	SignalErrNeedsParams SignalReadErrorCode = "needs_params"
	SignalErrUnsupported SignalReadErrorCode = "unsupported"
	SignalErrBadQuery    SignalReadErrorCode = "bad_query"
	SignalErrUpstream    SignalReadErrorCode = "upstream"
	SignalErrTimeout     SignalReadErrorCode = "timeout"
	SignalErrNotFound    SignalReadErrorCode = "not_found"
	SignalErrInternal    SignalReadErrorCode = "error"
)

type SignalReadError struct {
	Code    SignalReadErrorCode `json:"code"`
	Message string              `json:"message"`
}

// SignalSeries is one signal of a component series read. Previous is the
// window before, shifted onto this window's timestamps; empty unless asked
// for. A signal that failed carries Error and no series; the others stand.
// A previous window that failed carries PreviousError and leaves Series be.
// Live: the bound source reads only the value now (LiveOnlyKind), so a
// series holds one point however long the window.
type SignalSeries struct {
	Signal        string           `json:"signal"`
	Unit          string           `json:"unit"`
	Bound         bool             `json:"bound"`
	Live          bool             `json:"live,omitempty"`
	Series        []Series         `json:"series"`
	Previous      []Series         `json:"previous"`
	Error         *SignalReadError `json:"error"`
	PreviousError *SignalReadError `json:"previous_error"`
}

// ComponentSeries is GET /v1/components/:id/series. StepSeconds is the
// spacing the series were read at: the asked step, or coarser where a source
// can only answer that (CloudWatch's whole-minute periods).
type ComponentSeries struct {
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	StepSeconds int64          `json:"step_seconds"`
	Signals     []SignalSeries `json:"signals"`
}

// GoldenSignal is one golden measure of a component: Value the last non-null
// sample of the window (the p95 for a quantile signal), Previous the sample
// one window earlier, Spark the window itself. Bound tells a signal with no
// confirmed binding from one that has one but answered no data; Live one
// read from a source that holds only the value now (LiveOnlyKind), whose
// spark is one point.
type GoldenSignal struct {
	Signal   string        `json:"signal"`
	Unit     string        `json:"unit"`
	Bound    bool          `json:"bound"`
	Live     bool          `json:"live,omitempty"`
	Value    *float64      `json:"value"`
	Previous *float64      `json:"previous"`
	Spark    []SeriesPoint `json:"spark"`
}

type GoldenComponent struct {
	ComponentID uuid.UUID      `json:"component_id"`
	Signals     []GoldenSignal `json:"signals"`
}

// GoldenBoard is GET /v1/operations/golden.
type GoldenBoard struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Components  []GoldenComponent `json:"components"`
}
