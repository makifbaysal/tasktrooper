package datasource

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnsupported is a call the source's kind cannot answer (a query on a
	// kind without MetricsQuerier, a kind no longer registered).
	ErrUnsupported = errors.New("this data source kind does not support that")
	// ErrUpstream is the source failing or refusing a request; its message
	// says what it answered, never with a secret in it.
	ErrUpstream = errors.New("the data source request failed")
)

// Field error codes: what a FieldErrors value holds per key.
const (
	FieldRequired      = "required"
	FieldInvalid       = "invalid"
	FieldNotPermitted  = "not_permitted"
	FieldUnknown       = "unknown_field"
	FieldInvalidOption = "invalid_option"
	FieldTooLong       = "too_long"
	// FieldNotFound, on a cloud_account field: no connected account has that
	// id. FieldWrongProvider: the account is another provider's.
	FieldNotFound      = "not_found"
	FieldWrongProvider = "wrong_provider"
	// FieldSecretsRequired, on key "secrets": the PATCH moves the URL to
	// another origin and must re-send every stored secret it keeps.
	FieldSecretsRequired = "secrets_required"
)

// FieldErrors maps a request key (a config/secret field, "kind", "name") to a
// field error code.
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
	return "invalid data source: " + strings.Join(parts, ", ")
}

func (f FieldErrors) Is(target error) bool { return target == ErrInvalidInput }
