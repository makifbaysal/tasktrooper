package alerting

import (
	"errors"
	"sort"
	"strings"
)

var ErrInvalidInput = errors.New("invalid input")

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
