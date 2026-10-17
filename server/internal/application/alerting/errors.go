package alerting

import (
	"errors"
	"sort"
	"strings"
)

var ErrInvalidInput = errors.New("invalid input")

// FieldUnavailable, on key "channels": a channel kind this server has not
// registered, so a rule naming it would notify no one.
const FieldUnavailable = "unavailable"

// errReadPanicked is a read that panicked: this server's own fault, so the
// rule keeps its state rather than reading it as no data.
var errReadPanicked = errors.New("reading the rule's samples panicked")

// FieldErrors maps a request key ("name", "condition.op", "channels") to a
// datasource.Field* code, the data source routes' shape.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+f[k])
	}
	return "invalid alert rule: " + strings.Join(parts, ", ")
}

func (f FieldErrors) Is(target error) bool { return target == ErrInvalidInput }
