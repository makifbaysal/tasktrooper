package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LogsQuerier is a kind that reads log lines (Loki, Elasticsearch). Entries
// come newest first, at most q.Limit of them; a page that may hide older
// lines says so through Truncated or NextCursor. Secrets are passed as for
// MetricsQuerier.Series; an empty page is not an error.
type LogsQuerier interface {
	Logs(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.LogQuery) (domain.RuntimeLogPage, error)
}

// TracesQuerier is a kind that reads traces (Tempo). A trace id the source
// does not hold is ErrNotFound.
type TracesQuerier interface {
	Trace(ctx context.Context, ds domain.DataSource, secrets map[string]string, traceID string) (domain.Trace, error)
	SearchTraces(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TraceQuery) ([]domain.TraceSummary, error)
}

// LabelValuesLister answers the values one label (a Loki label, an
// Elasticsearch field, a Tempo tag) holds lately, for the detector to find
// the ones naming a component. The kind caps the list itself.
type LabelValuesLister interface {
	LabelValues(ctx context.Context, ds domain.DataSource, secrets map[string]string, label string) ([]string, error)
}

// BoundLogReader reads component logs through their confirmed logs.lines
// bindings, for the Logs explorer.
type BoundLogReader interface {
	// LogBindings answers the confirmed logs.lines binding of every component
	// of env that is not dismissed.
	LogBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBinding, error)
	// ReadLogs renders the binding's query and reads q through it; q.Query is
	// ignored.
	ReadLogs(ctx context.Context, b domain.SignalBinding, q domain.LogQuery) (domain.RuntimeLogPage, error)
}
