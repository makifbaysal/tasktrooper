package errordef

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotSuggested is accept or dismiss on a definition no one suggested,
	// or one already accepted or dismissed the other way.
	ErrNotSuggested = errors.New("the error definition is not a suggestion")
	// ErrTooManyDefinitions: the repository already holds MaxDefinitionsPerRepository
	// active or suggested definitions; every one is matched against every
	// group of every inbox read.
	ErrTooManyDefinitions = errors.New("the repository already has as many error definitions as it may hold")

	errRegexTooLong  = errors.New("the pattern is longer than 512 characters")
	errRegexTooLarge = errors.New("the pattern compiles to too large a program")
)

// FieldErrors maps a request key ("name", "match.message_regex") to a
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
	return "invalid error definition: " + strings.Join(parts, ", ")
}

func (f FieldErrors) Is(target error) bool { return target == ErrInvalidInput }
